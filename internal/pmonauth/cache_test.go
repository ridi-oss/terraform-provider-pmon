package pmonauth

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func testEntry() *entry {
	return &entry{
		TokenURL: "https://pmon.example.com/oauth/token",
		ClientID: "https://example.github.io/client-metadata.json",
		Scopes:   []string{"mcp:read"},
		Token: &oauth2.Token{
			AccessToken:  "access",
			RefreshToken: "refresh",
			TokenType:    "Bearer",
			Expiry:       time.Now().Add(time.Hour).Truncate(time.Second),
		},
	}
}

func TestCacheRoundTrip(t *testing.T) {
	c := newCache(filepath.Join(t.TempDir(), "nested", "token.json"))

	if got := c.load("https://pmon.example.com/mcp"); got != nil {
		t.Fatalf("load on an absent cache = %+v, want nil", got)
	}
	if err := c.save("https://pmon.example.com/mcp", testEntry()); err != nil {
		t.Fatalf("save: %v", err)
	}

	got := c.load("https://pmon.example.com/mcp")
	if got == nil {
		t.Fatal("load after save = nil")
	}
	if got.Token.AccessToken != "access" || got.Token.RefreshToken != "refresh" {
		t.Errorf("token round trip = %+v", got.Token)
	}
	if got.TokenURL != "https://pmon.example.com/oauth/token" {
		t.Errorf("token URL = %q, want it preserved so a refresh needs no rediscovery", got.TokenURL)
	}
}

// One workstation may talk to several pmon deployments, and their tokens must not collide.
func TestCacheSeparatesEndpoints(t *testing.T) {
	c := newCache(filepath.Join(t.TempDir(), "token.json"))

	first := testEntry()
	second := testEntry()
	second.Token.AccessToken = "other"

	if err := c.save("https://a.example.com/mcp", first); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := c.save("https://b.example.com/mcp", second); err != nil {
		t.Fatalf("save: %v", err)
	}

	if got := c.load("https://a.example.com/mcp"); got == nil || got.Token.AccessToken != "access" {
		t.Errorf("first endpoint = %+v, want its own token", got)
	}
	if got := c.load("https://b.example.com/mcp"); got == nil || got.Token.AccessToken != "other" {
		t.Errorf("second endpoint = %+v, want its own token", got)
	}
}

func TestCacheDrop(t *testing.T) {
	c := newCache(filepath.Join(t.TempDir(), "token.json"))
	if err := c.save("https://pmon.example.com/mcp", testEntry()); err != nil {
		t.Fatalf("save: %v", err)
	}

	c.drop("https://pmon.example.com/mcp")

	if got := c.load("https://pmon.example.com/mcp"); got != nil {
		t.Errorf("load after drop = %+v, want nil", got)
	}
}

// The file holds a bearer token, so it must not be group or world readable.
func TestCacheFileIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	c := newCache(path)
	if err := c.save("https://pmon.example.com/mcp", testEntry()); err != nil {
		t.Fatalf("save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache file mode = %o, want 600", perm)
	}
}

// A corrupt cache should cost one fresh login, not break every future run.
func TestCacheTreatsCorruptFileAsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seeding a corrupt cache: %v", err)
	}
	c := newCache(path)

	if got := c.load("https://pmon.example.com/mcp"); got != nil {
		t.Errorf("load on a corrupt cache = %+v, want nil", got)
	}
	if err := c.save("https://pmon.example.com/mcp", testEntry()); err != nil {
		t.Errorf("save over a corrupt cache: %v", err)
	}
	if got := c.load("https://pmon.example.com/mcp"); got == nil {
		t.Error("save over a corrupt cache did not take")
	}
}
