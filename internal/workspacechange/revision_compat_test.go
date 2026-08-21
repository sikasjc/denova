package workspacechange

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestApplyEditsRejectsNonCanonicalRevision(t *testing.T) {
	canonical := Revision([]byte("alpha beta gamma"))
	service, path := newTestServiceWithFile(t, "alpha beta gamma")
	_, err := service.ApplyEdits(context.Background(), ApplyEditsRequest{
		Path:         path,
		BaseRevision: "sha256:" + canonical,
		Edits:        []TextEdit{{StartLine: 1, NewString: "ALPHA beta gamma"}},
		Metadata:     ChangeMetadata{Origin: OriginAgent},
	})
	var changeErr *Error
	if !errors.As(err, &changeErr) || changeErr.Code != ErrorCodeRevisionConflict {
		t.Fatalf("non-canonical revision should conflict, got %v", err)
	}
	if changeErr.Details["expected_revision"] != "sha256:"+canonical || changeErr.Details["actual_revision"] != canonical {
		t.Fatalf("conflict details = %#v", changeErr.Details)
	}
}

// A real conflict must still be rejected, and the error has to name both
// revisions: that is the only signal that distinguishes "you mangled the token"
// from "the file really moved", and without it a model loops on re-reads.
func TestApplyEditsConflictNamesBothRevisions(t *testing.T) {
	service, path := newTestServiceWithFile(t, "current content")
	stale := Revision([]byte("stale content"))
	_, err := service.ApplyEdits(context.Background(), ApplyEditsRequest{
		Path:         path,
		BaseRevision: stale,
		Edits:        []TextEdit{{StartLine: 1, NewString: "replacement"}},
		Metadata:     ChangeMetadata{Origin: OriginAgent},
	})
	var changeErr *Error
	if !errors.As(err, &changeErr) || changeErr.Code != ErrorCodeRevisionConflict {
		t.Fatalf("stale revision should conflict, got %v", err)
	}
	actual := Revision([]byte("current content"))
	if !strings.Contains(changeErr.Message, stale) || !strings.Contains(changeErr.Message, actual) {
		t.Fatalf("conflict message must name both revisions: %s", changeErr.Message)
	}
	if changeErr.Details["expected_revision"] != stale || changeErr.Details["actual_revision"] != actual {
		t.Fatalf("conflict details are not canonical: %#v", changeErr.Details)
	}
	if !strings.Contains(changeErr.Message, "revision 冲突") {
		t.Fatalf("conflict message should be compact Chinese: %s", changeErr.Message)
	}
}

// One call carrying several line edits is the supported way to make many changes
// at once: all selectors resolve against the same base snapshot, so the model
// does not have to predict how earlier edits shifted later line numbers, and the
// revision advances exactly once for the whole batch.
func TestApplyEditsBatchesManyLineEditsAgainstOneRevision(t *testing.T) {
	original := "line one\nline two\nline three\nline four\n"
	service, path := newTestServiceWithFile(t, original)
	base := Revision([]byte(original))
	change, err := service.ApplyEdits(context.Background(), ApplyEditsRequest{
		Path:         path,
		BaseRevision: base,
		Edits: []TextEdit{
			{StartLine: 1, NewString: "LINE ONE"},
			{StartLine: 3, NewString: "LINE THREE\nLINE THREE B"},
			{StartLine: 4, NewString: "LINE FOUR"},
		},
		Metadata: ChangeMetadata{Origin: OriginAgent},
	})
	if err != nil {
		t.Fatalf("batched line edits failed: %v", err)
	}
	want := "LINE ONE\nline two\nLINE THREE\nLINE THREE B\nLINE FOUR\n"
	if got := readTestFile(t, service.workspace, path); got != want {
		t.Fatalf("batch result = %q, want %q", got, want)
	}
	if change.BaseRevision != base || change.Revision != Revision([]byte(want)) {
		t.Fatalf("batch should advance the revision exactly once: %#v", change)
	}
	if len(change.Edits) != 3 {
		t.Fatalf("expected 3 recorded edits, got %d", len(change.Edits))
	}
}
