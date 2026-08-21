package revisionfile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRevisionIsBareLowercaseHex(t *testing.T) {
	revision := Revision([]byte("content"))
	if len(revision) != 64 {
		t.Fatalf("revision length = %d, want 64: %q", len(revision), revision)
	}
	if strings.Contains(revision, ":") {
		t.Fatalf("revision must not carry an algorithm prefix: %q", revision)
	}
	if revision != strings.ToLower(revision) {
		t.Fatalf("revision must be lowercase: %q", revision)
	}
}

func TestReplaceIfRevisionRejectsNonCanonicalRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.toml")
	if err := os.WriteFile(path, []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := "sha256:" + Revision([]byte("base"))
	_, err := ReplaceIfRevision(context.Background(), path, legacy, []byte("next"), Options{})
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("non-canonical revision should conflict, got %v", err)
	}
	if conflict.Expected != legacy || conflict.Actual != Revision([]byte("base")) {
		t.Fatalf("conflict pair = %#v", conflict)
	}
}
