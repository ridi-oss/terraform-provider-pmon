package pmonauth

import (
	"errors"
	"strings"
	"testing"
)

const pmonChallenge = `Bearer resource_metadata="https://pmon.example.com/.well-known/oauth-protected-resource", scope="mcp:read"`

func TestNarrowChallengeWithoutCeiling(t *testing.T) {
	got, err := narrowChallenge([]string{pmonChallenge}, nil)
	if err != nil {
		t.Fatalf("narrowChallenge: %v", err)
	}
	if len(got) != 1 || got[0] != pmonChallenge {
		t.Errorf("narrowChallenge = %q, want the header untouched", got)
	}
}

func TestNarrowChallengeIntersects(t *testing.T) {
	header := `Bearer error="insufficient_scope", scope="mcp:read mcp:identity:write"`

	got, err := narrowChallenge([]string{header}, []string{"mcp:read", "mcp:policies:write"})
	if err != nil {
		t.Fatalf("narrowChallenge: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("narrowChallenge returned %d headers, want 1", len(got))
	}
	if !strings.Contains(got[0], `scope="mcp:read"`) {
		t.Errorf("narrowChallenge = %q, want only the allowed scope", got[0])
	}
	if strings.Contains(got[0], "mcp:identity:write") {
		t.Errorf("narrowChallenge = %q, want the disallowed scope gone", got[0])
	}
}

// A challenge that names no scope would otherwise let the SDK fall back to every scope the
// protected resource advertises, which is the whole set the ceiling exists to cut down.
func TestNarrowChallengeFillsAnUnscopedChallenge(t *testing.T) {
	got, err := narrowChallenge([]string{`Bearer realm="pmon"`}, []string{"mcp:read"})
	if err != nil {
		t.Fatalf("narrowChallenge: %v", err)
	}
	if len(got) != 1 || !strings.Contains(got[0], `scope="mcp:read"`) {
		t.Errorf("narrowChallenge = %q, want the ceiling written into the challenge", got)
	}
}

func TestNarrowChallengeRejectsDisallowedScope(t *testing.T) {
	header := `Bearer error="insufficient_scope", scope="mcp:identity:write"`

	_, err := narrowChallenge([]string{header}, []string{"mcp:read"})
	if !errors.Is(err, ErrScopeNotAllowed) {
		t.Fatalf("narrowChallenge error = %v, want ErrScopeNotAllowed", err)
	}
	for _, want := range []string{"mcp:identity:write", "mcp:read"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

// resource_metadata is how the SDK finds the authorization server. Losing it to the rewrite would
// break the login itself.
func TestNarrowChallengePreservesOtherParams(t *testing.T) {
	got, err := narrowChallenge([]string{pmonChallenge}, []string{"mcp:read"})
	if err != nil {
		t.Fatalf("narrowChallenge: %v", err)
	}
	want := `resource_metadata="https://pmon.example.com/.well-known/oauth-protected-resource"`
	if !strings.Contains(got[0], want) {
		t.Errorf("narrowChallenge = %q, want it to keep %s", got[0], want)
	}
}

func TestNarrowChallengeLeavesOtherSchemesAlone(t *testing.T) {
	got, err := narrowChallenge([]string{`Basic realm="pmon"`}, []string{"mcp:read"})
	if err != nil {
		t.Fatalf("narrowChallenge: %v", err)
	}
	if len(got) != 1 || strings.Contains(got[0], "scope=") {
		t.Errorf("narrowChallenge = %q, want a non-bearer challenge untouched", got)
	}
}

// A malformed header is the SDK's to report. Rewriting it would replace a precise parse error
// with a confusing one.
func TestNarrowChallengePassesThroughUnparseableHeaders(t *testing.T) {
	header := `Bearer scope="unterminated`

	got, err := narrowChallenge([]string{header}, []string{"mcp:read"})
	if err != nil {
		t.Fatalf("narrowChallenge: %v", err)
	}
	if len(got) != 1 || got[0] != header {
		t.Errorf("narrowChallenge = %q, want the header handed back as it came", got)
	}
}

func TestScopesWithin(t *testing.T) {
	for name, tc := range map[string]struct {
		have, allowed []string
		want          bool
	}{
		"no ceiling": {[]string{"mcp:read", "mcp:identity:write"}, nil, true},
		"subset":     {[]string{"mcp:read"}, []string{"mcp:read", "mcp:policies:write"}, true},
		"exact":      {[]string{"mcp:read"}, []string{"mcp:read"}, true},
		"over ceiling": {
			[]string{"mcp:read", "mcp:identity:write"},
			[]string{"mcp:read"},
			false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := scopesWithin(tc.have, tc.allowed); got != tc.want {
				t.Errorf("scopesWithin = %v, want %v", got, tc.want)
			}
		})
	}
}
