package provider

import "testing"

// The key must depend only on the intended end state: pmon replays a recorded outcome for a
// repeated key, which is what makes a retry safe, and a key that varied per attempt would defeat
// that entirely.
func TestIdempotencyKeyIsStable(t *testing.T) {
	first := idempotencyKey("policy.create", "service:fence", "forbid();")
	second := idempotencyKey("policy.create", "service:fence", "forbid();")

	if first != second {
		t.Errorf("same inputs produced %s and %s", first, second)
	}
}

func TestIdempotencyKeyVariesWithIntent(t *testing.T) {
	base := idempotencyKey("policy.create", "service:fence", "forbid();")

	for name, other := range map[string]string{
		"different source":    idempotencyKey("policy.create", "service:fence", "permit();"),
		"different name":      idempotencyKey("policy.create", "other", "forbid();"),
		"different operation": idempotencyKey("policy.update", "service:fence", "forbid();"),
	} {
		if other == base {
			t.Errorf("%s produced the same key as the base", name)
		}
	}
}

// Joining on a separator that cannot occur in a name keeps neighbouring fields from running
// together into the same key.
func TestIdempotencyKeySeparatesParts(t *testing.T) {
	if idempotencyKey("ab", "c") == idempotencyKey("a", "bc") {
		t.Error("adjacent parts collided")
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
