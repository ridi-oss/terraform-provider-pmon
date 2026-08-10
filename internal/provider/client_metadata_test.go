package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// clientMetadata mirrors the fields pmon's CIMD resolver validates.
type clientMetadata struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

func loadClientMetadata(t *testing.T) (clientMetadata, []byte) {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "client-metadata.json"))
	if err != nil {
		t.Fatalf("reading client-metadata.json: %v", err)
	}
	var doc clientMetadata
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsing client-metadata.json: %v", err)
	}
	return doc, raw
}

// The document is this provider's OAuth identity, so a drifting constant would break login
// with a 400 from pmon rather than anything obviously wrong in the code.
func TestClientMetadataMatchesDefaultURL(t *testing.T) {
	doc, _ := loadClientMetadata(t)

	if doc.ClientID != DefaultClientMetadataURL {
		t.Errorf("client_id = %q, want %q (the URL it is served from)", doc.ClientID, DefaultClientMetadataURL)
	}
	if doc.ClientName == "" {
		t.Error("client_name must not be blank")
	}
}

// pmon accepts public clients only.
func TestClientMetadataIsAPublicClient(t *testing.T) {
	doc, _ := loadClientMetadata(t)

	if doc.TokenEndpointAuthMethod != "none" {
		t.Errorf("token_endpoint_auth_method = %q, want \"none\"", doc.TokenEndpointAuthMethod)
	}
	if !slices.Contains(doc.ResponseTypes, "code") {
		t.Errorf("response_types = %v, want it to contain \"code\"", doc.ResponseTypes)
	}
	if !slices.Contains(doc.GrantTypes, "authorization_code") {
		t.Errorf("grant_types = %v, want it to contain \"authorization_code\"", doc.GrantTypes)
	}
	if !slices.Contains(doc.GrantTypes, "refresh_token") {
		t.Errorf("grant_types = %v, want it to contain \"refresh_token\"", doc.GrantTypes)
	}
}

// A loopback redirect_uri that names a port is matched exactly, and the provider binds an
// ephemeral port at login, so a declared port could never match.
func TestClientMetadataLoopbackRedirectsArePortless(t *testing.T) {
	doc, _ := loadClientMetadata(t)

	if len(doc.RedirectURIs) == 0 {
		t.Fatal("redirect_uris must not be empty")
	}
	for _, uri := range doc.RedirectURIs {
		if !strings.HasPrefix(uri, "http://") {
			continue
		}
		host, _, found := strings.Cut(strings.TrimPrefix(uri, "http://"), "/")
		if !found {
			t.Errorf("redirect_uri %q has no path", uri)
			continue
		}
		if strings.Contains(host, ":") {
			t.Errorf("loopback redirect_uri %q names a port; pmon then requires an exact match", uri)
		}
	}
}

// Every scope the provider may request has to be declared, or pmon rejects the authorization
// request outright.
func TestClientMetadataDeclaresEveryDefaultScope(t *testing.T) {
	doc, _ := loadClientMetadata(t)

	declared := strings.Fields(doc.Scope)
	for _, scope := range DefaultScopes {
		if !slices.Contains(declared, scope) {
			t.Errorf("scope %q is requested by default but not declared in the document", scope)
		}
	}
}

func TestClientMetadataFitsFetchLimit(t *testing.T) {
	_, raw := loadClientMetadata(t)

	const maxDocumentBytes = 5 * 1024
	if len(raw) > maxDocumentBytes {
		t.Errorf("document is %d bytes, over pmon's %d byte limit", len(raw), maxDocumentBytes)
	}
}
