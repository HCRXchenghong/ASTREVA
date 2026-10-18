package store

import "testing"

func TestProtectRevealSecret(t *testing.T) {
	protected, err := protectSecret("test-key", "sk-test-value")
	if err != nil {
		t.Fatalf("protect secret: %v", err)
	}
	if protected == "sk-test-value" {
		t.Fatal("secret was not encrypted")
	}
	revealed, err := revealSecret("test-key", protected)
	if err != nil {
		t.Fatalf("reveal secret: %v", err)
	}
	if revealed != "sk-test-value" {
		t.Fatalf("revealed value mismatch: %q", revealed)
	}
}

func TestProtectSecretWithoutKeyIsRejected(t *testing.T) {
	if _, err := protectSecret("", "sk-dev"); err == nil {
		t.Fatal("secret must never fall back to plaintext when encryption key is missing")
	}
}
