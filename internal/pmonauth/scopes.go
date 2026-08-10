package pmonauth

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// ErrScopeNotAllowed reports that pmon demanded a scope the operator did not allow.
var ErrScopeNotAllowed = errors.New("pmon requires a scope this provider is not allowed to request")

// narrowChallenge rewrites a WWW-Authenticate header so the login asks for no more than allowed.
//
// The SDK takes the scope set it will request straight from this header, and falls back to every
// scope the protected resource advertises when the challenge names none. It exposes no scope
// configuration of its own, so the ceiling has to be applied to the challenge before the
// authorization-code handler ever reads it.
//
// An empty ceiling means no ceiling, which is the default.
func narrowChallenge(headers, allowed []string) ([]string, error) {
	if len(allowed) == 0 || len(headers) == 0 {
		return slices.Clone(headers), nil
	}

	challenges, err := oauthex.ParseWWWAuthenticate(headers)
	if err != nil {
		// The SDK is about to parse the same header and fail the same way. Hand back the original
		// so the operator sees the SDK's own error rather than a mangled rewrite of it.
		return slices.Clone(headers), nil //nolint:nilerr // the SDK reports this same parse failure
	}

	rewritten := make([]string, 0, len(challenges))
	for _, challenge := range challenges {
		if challenge.Scheme != "bearer" {
			rewritten = append(rewritten, formatChallenge(challenge))
			continue
		}

		granted := allowed
		if asked := strings.Fields(challenge.Params["scope"]); len(asked) > 0 {
			granted = intersectScopes(asked, allowed)
			if len(granted) == 0 {
				// Requesting nothing would succeed and then fail every call with
				// insufficient_scope, which reads nothing like a ceiling that is too tight.
				return nil, fmt.Errorf("%w: it asked for %q, and scopes allows only %q",
					ErrScopeNotAllowed, strings.Join(asked, " "), strings.Join(allowed, " "))
			}
		}

		if challenge.Params == nil {
			challenge.Params = map[string]string{}
		}
		challenge.Params["scope"] = strings.Join(granted, " ")
		rewritten = append(rewritten, formatChallenge(challenge))
	}
	return rewritten, nil
}

// scopesWithin reports whether every scope in have sits under the ceiling. No ceiling allows all.
func scopesWithin(have, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, scope := range have {
		if !slices.Contains(allowed, scope) {
			return false
		}
	}
	return true
}

func intersectScopes(asked, allowed []string) []string {
	kept := make([]string, 0, len(asked))
	for _, scope := range asked {
		if slices.Contains(allowed, scope) {
			kept = append(kept, scope)
		}
	}
	return kept
}

var challengeValueEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// formatChallenge renders a parsed challenge back into header form. Parameters come out sorted
// because a map has no order of its own, and every one of them is carried over: the SDK reads
// resource_metadata to discover the authorization server and error to build its diagnostic, so
// dropping either would break the login in a way the scope rewrite has no business causing.
func formatChallenge(challenge oauthex.Challenge) string {
	if len(challenge.Params) == 0 {
		return challenge.Scheme
	}
	parts := make([]string, 0, len(challenge.Params))
	for _, key := range slices.Sorted(maps.Keys(challenge.Params)) {
		parts = append(parts, key+`="`+challengeValueEscaper.Replace(challenge.Params[key])+`"`)
	}
	return challenge.Scheme + " " + strings.Join(parts, ", ")
}
