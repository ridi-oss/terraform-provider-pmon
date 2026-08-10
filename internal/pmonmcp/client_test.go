package pmonmcp

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestResultPayloadPrefersStructuredContent(t *testing.T) {
	result := &mcp.CallToolResult{
		StructuredContent: map[string]any{"result": []string{"a"}},
		Content:           []mcp.Content{&mcp.TextContent{Text: "ignored"}},
	}

	got := string(resultPayload(result))
	if got != `{"result":["a"]}` {
		t.Errorf("resultPayload = %s, want the structured content", got)
	}
}

func TestResultPayloadFallsBackToText(t *testing.T) {
	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: `{"result":`},
			&mcp.TextContent{Text: `[1,2]}`},
		},
	}

	got := string(resultPayload(result))
	if got != `{"result":[1,2]}` {
		t.Errorf("resultPayload = %s, want the concatenated text content", got)
	}
}

func TestUnwrapResult(t *testing.T) {
	for name, tc := range map[string]struct{ in, want string }{
		"envelope":    {`{"result":[1,2]}`, `[1,2]`},
		"bare array":  {`[1,2]`, `[1,2]`},
		"bare object": {`{"name":"analyst"}`, `{"name":"analyst"}`},
		"null result": {`{"result":null}`, `null`},
		"not json":    {`plain text`, `plain text`},
	} {
		t.Run(name, func(t *testing.T) {
			if got := string(unwrapResult([]byte(tc.in))); got != tc.want {
				t.Errorf("unwrapResult(%s) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

// The envelope must be peeled before decoding, or every list tool would decode to a zero value.
func TestUnwrapResultThenDecode(t *testing.T) {
	var roles []struct {
		Name string `json:"name"`
	}
	payload := []byte(`{"result":[{"name":"analyst"},{"name":"auditor"}]}`)

	if err := json.Unmarshal(unwrapResult(payload), &roles); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(roles) != 2 || roles[0].Name != "analyst" {
		t.Errorf("roles = %+v", roles)
	}
}
