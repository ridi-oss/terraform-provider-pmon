package pmonmcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Error codes pmon returns for conditions a resource has to handle rather than merely report.
const (
	// CodePolicySystemImmutable is returned for an attempt to write a shipped SYSTEM policy.
	CodePolicySystemImmutable = "policy.system_immutable"
	// CodeRoleSystemImmutable is returned for an attempt to write a shipped SYSTEM role.
	CodeRoleSystemImmutable = "role.system_immutable"
	// CodeGroupSystemImmutable is returned for an attempt to write a shipped SYSTEM group.
	CodeGroupSystemImmutable = "group.system_immutable"
)

// ToolError is a tool call pmon rejected. MCP reports these in band with isError rather than as a
// protocol error, so they arrive as a successful call carrying a failure.
type ToolError struct {
	// Tool is the tool that failed.
	Tool string
	// Code is pmon's error code when the response carries one, otherwise empty.
	Code string
	// Message is the human-readable failure.
	Message string
}

func (e *ToolError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Tool, e.Message, e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Tool, e.Message)
}

// HasCode reports whether err is a ToolError carrying code.
func HasCode(err error, code string) bool {
	var toolErr *ToolError
	if !errors.As(err, &toolErr) {
		return false
	}
	return toolErr.Code == code
}

// IsSystemImmutable reports whether err is pmon refusing to modify a shipped SYSTEM row. A
// resource should surface this as a configuration mistake, not retry it.
func IsSystemImmutable(err error) bool {
	return HasCode(err, CodePolicySystemImmutable) ||
		HasCode(err, CodeRoleSystemImmutable) ||
		HasCode(err, CodeGroupSystemImmutable)
}

// codePattern matches pmon's dotted error codes, e.g. policy.system_immutable. Codes are not
// carried in a dedicated field, so they are recovered from the message on a best-effort basis:
// a miss costs the caller a specific check, never correctness.
var codePattern = regexp.MustCompile(`\b[a-z][a-z0-9]*(?:\.[a-z][a-z0-9_]*)+\b`)

func newToolError(tool string, payload []byte) *ToolError {
	message := strings.TrimSpace(string(payload))

	// A structured failure is the common shape; fall back to the raw text when it is not.
	var structured struct {
		Error   string `json:"error"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &structured); err == nil {
		switch {
		case structured.Message != "":
			message = structured.Message
		case structured.Error != "":
			message = structured.Error
		}
		if structured.Code != "" {
			return &ToolError{Tool: tool, Code: structured.Code, Message: message}
		}
	}

	if message == "" {
		message = "the tool reported an error with no detail"
	}
	return &ToolError{Tool: tool, Code: codePattern.FindString(message), Message: message}
}
