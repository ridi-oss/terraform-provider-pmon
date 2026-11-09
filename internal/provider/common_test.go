package provider

import "testing"

func TestEveryWriteGetsAFreshIdempotencyKey(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		key := newIdempotencyKey()
		if len(key) != 32 || seen[key] {
			t.Fatalf("key %q repeated or not 128 bits of hex", key)
		}
		seen[key] = true
	}
}

func TestIsReservedName(t *testing.T) {
	for name, want := range map[string]bool{
		"system:production-viewer": true,
		"system:":                  true,
		"service:alpha":            false,
		"analyst":                  false,
		"my-system:role":           false,
	} {
		if got := isReservedName(name); got != want {
			t.Errorf("isReservedName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestIsNotFound(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"nil":          {nil, false},
		"not found":    {errString("policy not found"), true},
		"snake case":   {errString("policy.not_found"), true},
		"no such":      {errString("no such role"), true},
		"unrelated":    {errString("name is already taken"), false},
		"mixed casing": {errString("Policy Not Found"), true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := isNotFound(tc.err); got != tc.want {
				t.Errorf("isNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

type errString string

func (e errString) Error() string { return string(e) }
