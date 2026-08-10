package pmonmcp

import (
	"errors"
	"fmt"
	"testing"
)

func TestNewToolErrorReadsStructuredCode(t *testing.T) {
	err := newToolError("update_policy", []byte(`{"code":"policy.system_immutable","message":"SYSTEM policies are immutable"}`))

	if err.Code != CodePolicySystemImmutable {
		t.Errorf("code = %q, want %q", err.Code, CodePolicySystemImmutable)
	}
	if err.Message != "SYSTEM policies are immutable" {
		t.Errorf("message = %q", err.Message)
	}
}

// pmon does not always carry a code in its own field, so it is recovered from the message.
func TestNewToolErrorRecoversCodeFromMessage(t *testing.T) {
	err := newToolError("delete_role", []byte("role.system_immutable: cannot delete a SYSTEM role"))

	if err.Code != CodeRoleSystemImmutable {
		t.Errorf("code = %q, want %q", err.Code, CodeRoleSystemImmutable)
	}
}

func TestNewToolErrorWithoutCode(t *testing.T) {
	err := newToolError("create_role", []byte("name is already taken"))

	if err.Code != "" {
		t.Errorf("code = %q, want empty", err.Code)
	}
	if err.Message != "name is already taken" {
		t.Errorf("message = %q", err.Message)
	}
}

func TestNewToolErrorWithEmptyPayload(t *testing.T) {
	err := newToolError("create_role", nil)

	if err.Message == "" {
		t.Error("an empty payload must still produce a message")
	}
}

func TestHasCodeThroughWrapping(t *testing.T) {
	err := fmt.Errorf("creating the role: %w", newToolError("create_role", []byte(`{"code":"role.system_immutable"}`)))

	if !HasCode(err, CodeRoleSystemImmutable) {
		t.Error("HasCode did not see through the wrapping")
	}
	if !IsSystemImmutable(err) {
		t.Error("IsSystemImmutable did not see through the wrapping")
	}
	if HasCode(errors.New("unrelated"), CodeRoleSystemImmutable) {
		t.Error("HasCode matched a plain error")
	}
}
