package pmonauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/oauth2"
)

// entry is one endpoint's cached credential. The token endpoint, client id, and scopes are
// stored beside the token because refreshing needs them, and rediscovering them would mean a
// round trip to the authorization server on every terraform invocation.
type entry struct {
	TokenURL string        `json:"token_url"`
	ClientID string        `json:"client_id"`
	Scopes   []string      `json:"scopes,omitempty"`
	Token    *oauth2.Token `json:"token"`
}

// cache is a small JSON file mapping MCP endpoint to credential, so one workstation can hold
// tokens for several pmon deployments without them colliding.
type cache struct {
	path string
	mu   sync.Mutex
}

func newCache(path string) *cache {
	return &cache{path: path}
}

// load returns the cached entry for endpoint, or nil when there is nothing usable. A cache that
// cannot be read or parsed is treated as absent: a corrupt file should cost a fresh login, not
// break the provider.
func (c *cache) load(endpoint string) *entry {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := c.readLocked()
	if err != nil {
		return nil
	}
	e, ok := entries[endpoint]
	if !ok || e.Token == nil || e.Token.AccessToken == "" {
		return nil
	}
	return &e
}

func (c *cache) save(endpoint string, e *entry) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := c.readLocked()
	if err != nil {
		entries = map[string]entry{}
	}
	entries[endpoint] = *e
	return c.writeLocked(entries)
}

func (c *cache) drop(endpoint string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, err := c.readLocked()
	if err != nil {
		return
	}
	if _, ok := entries[endpoint]; !ok {
		return
	}
	delete(entries, endpoint)
	_ = c.writeLocked(entries)
}

func (c *cache) readLocked() (map[string]entry, error) {
	raw, err := os.ReadFile(c.path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]entry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var entries map[string]entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parsing token cache %s: %w", c.path, err)
	}
	if entries == nil {
		entries = map[string]entry{}
	}
	return entries, nil
}

// writeLocked replaces the file atomically. Two terraform runs refreshing at once still race for
// which refresh token survives -- the authorization server rotates on every use, so that race is
// not ours to win -- but neither can leave a half-written file behind.
func (c *cache) writeLocked(entries map[string]entry) error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return fmt.Errorf("creating token cache directory: %w", err)
	}

	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding token cache: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(c.path), filepath.Base(c.path)+".*")
	if err != nil {
		return fmt.Errorf("creating token cache temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("securing token cache temp file: %w", err)
	}
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing token cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing token cache temp file: %w", err)
	}
	if err := os.Rename(tmp.Name(), c.path); err != nil {
		return fmt.Errorf("replacing token cache: %w", err)
	}
	return nil
}
