package workspacechange

import (
	"context"
	"testing"
)

func TestApplyEditsRecordsHunkLineNumbers(t *testing.T) {
	service, path := newTestServiceWithFile(t, "one\ntwo\nthree\nfour\nfive\n")
	change, err := service.ApplyEdits(context.Background(), ApplyEditsRequest{
		Path:         path,
		BaseRevision: Revision([]byte("one\ntwo\nthree\nfour\nfive\n")),
		Edits: []TextEdit{
			{ID: "line-edit", StartLine: 2, EndLine: 3, NewString: "TWO\nTWO-B\nTWO-C"},
			{ID: "exact-edit", OldString: "five", NewString: "FIVE"},
		},
		Metadata: ChangeMetadata{Origin: OriginAgent, ChangeGroupID: "line-group"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(change.Edits) != 2 {
		t.Fatalf("expected two applied edits: %#v", change.Edits)
	}
	// The line edit replaced lines 2-3 with three lines, shifting everything
	// after it down by one; the exact edit keeps its single-line span.
	expected := map[string]Hunk{
		"line-edit": {
			BeforeStartLine: 2, BeforeEndLine: 3,
			AfterStartLine: 2, AfterEndLine: 4,
		},
		"exact-edit": {
			BeforeStartLine: 5, BeforeEndLine: 5,
			AfterStartLine: 6, AfterEndLine: 6,
		},
	}
	for _, edit := range change.Edits {
		want, ok := expected[edit.ID]
		if !ok || len(edit.Hunks) != 1 {
			t.Fatalf("unexpected edit %q: %#v", edit.ID, edit)
		}
		got := edit.Hunks[0]
		if got.BeforeStartLine != want.BeforeStartLine || got.BeforeEndLine != want.BeforeEndLine ||
			got.AfterStartLine != want.AfterStartLine || got.AfterEndLine != want.AfterEndLine {
			t.Fatalf("edit %q line metadata = %+v, want %+v", edit.ID, got, want)
		}
	}
	if got := readTestFile(t, service.workspace, path); got != "one\nTWO\nTWO-B\nTWO-C\nfour\nFIVE\n" {
		t.Fatalf("edited content = %q", got)
	}
}

func TestApplyEditsRecordsHunkLineNumbersForReplaceAll(t *testing.T) {
	service, path := newTestServiceWithFile(t, "a\nx\nb\nx\nc\nx\n")
	change, err := service.ApplyEdits(context.Background(), ApplyEditsRequest{
		Path:         path,
		BaseRevision: Revision([]byte("a\nx\nb\nx\nc\nx\n")),
		Edits: []TextEdit{
			{ID: "all-x", OldString: "x", NewString: "XX\nXX", ReplaceAll: true},
		},
		Metadata: ChangeMetadata{Origin: OriginAgent, ChangeGroupID: "replaceAll-group"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(change.Edits) != 1 || len(change.Edits[0].Hunks) != 3 {
		t.Fatalf("expected three hunks for replace_all: %#v", change.Edits)
	}
	// Each hunk must carry its own line span: lines 2, 4, 6 before; the first
	// replacement inserts an extra line, shifting the later hunks.
	expected := []Hunk{
		{BeforeStartLine: 2, BeforeEndLine: 2, AfterStartLine: 2, AfterEndLine: 3},
		{BeforeStartLine: 4, BeforeEndLine: 4, AfterStartLine: 5, AfterEndLine: 6},
		{BeforeStartLine: 6, BeforeEndLine: 6, AfterStartLine: 8, AfterEndLine: 9},
	}
	for index, hunk := range change.Edits[0].Hunks {
		want := expected[index]
		if hunk.BeforeStartLine != want.BeforeStartLine || hunk.BeforeEndLine != want.BeforeEndLine ||
			hunk.AfterStartLine != want.AfterStartLine || hunk.AfterEndLine != want.AfterEndLine {
			t.Fatalf("hunk %d line metadata = %+v, want %+v", index, hunk, want)
		}
	}
	if got := readTestFile(t, service.workspace, path); got != "a\nXX\nXX\nb\nXX\nXX\nc\nXX\nXX\n" {
		t.Fatalf("edited content = %q", got)
	}
}

func TestReplaceFileRecordsHunkLineNumbers(t *testing.T) {
	service, path := newTestServiceWithFile(t, "one\ntwo\n")
	change, err := service.ReplaceFile(context.Background(), ReplaceFileRequest{
		Path:         path,
		Content:      "uno\ndos\ntres\n",
		BaseRevision: Revision([]byte("one\ntwo\n")),
		Metadata:     ChangeMetadata{Origin: OriginAgent, ChangeGroupID: "replace-group"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(change.Edits) != 1 || len(change.Edits[0].Hunks) != 1 {
		t.Fatalf("expected one full-file edit: %#v", change.Edits)
	}
	hunk := change.Edits[0].Hunks[0]
	if hunk.BeforeStartLine != 1 || hunk.BeforeEndLine != 2 || hunk.AfterStartLine != 1 || hunk.AfterEndLine != 3 {
		t.Fatalf("replace line metadata = %+v", hunk)
	}
}

func TestBatchedRedoInvalidationSurvivesRestart(t *testing.T) {
	workspace := t.TempDir()
	service, err := NewService(workspace)
	if err != nil {
		t.Fatal(err)
	}
	path := "chapters/ch01.md"
	writeTestFile(t, workspace, path, "v0")
	apply := func(groupID, after string) ChangeSet {
		change, err := service.ApplyEdits(context.Background(), ApplyEditsRequest{
			Path:         path,
			BaseRevision: Revision([]byte("v0")),
			Edits:        []TextEdit{{OldString: "v0", NewString: after}},
			Metadata:     ChangeMetadata{Origin: OriginAgent, ChangeGroupID: groupID},
		})
		if err != nil {
			t.Fatal(err)
		}
		return change
	}
	apply("group-a", "v1")
	if _, err := service.Undo(context.Background(), HistoryRequest{GroupID: "group-a"}); err != nil {
		t.Fatal(err)
	}
	apply("group-b", "v2")
	if _, err := service.Undo(context.Background(), HistoryRequest{GroupID: "group-b"}); err != nil {
		t.Fatal(err)
	}
	// A later change invalidates both undone groups through one batched ledger
	// event and then itself gets undone, restoring the shared base revision.
	apply("group-c", "v3")
	if _, err := service.Undo(context.Background(), HistoryRequest{GroupID: "group-c"}); err != nil {
		t.Fatal(err)
	}
	if got := readTestFile(t, workspace, path); got != "v0" {
		t.Fatalf("workspace should be back at the base: %q", got)
	}
	// The head matches again, yet redo must stay invalid for the superseded
	// groups — including after a full ledger replay.
	reloaded, err := NewService(workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, groupID := range []string{"group-a", "group-b"} {
		group, err := reloaded.GetGroup(context.Background(), groupID)
		if err != nil {
			t.Fatal(err)
		}
		if group.CanRedo {
			t.Fatalf("group %s redo must stay invalidated after restart", groupID)
		}
		if _, err := reloaded.Redo(context.Background(), HistoryRequest{GroupID: groupID}); err == nil {
			t.Fatalf("group %s redo should be rejected after restart", groupID)
		}
	}
	// The most recent undo remains redoable because it is not superseded.
	group, err := reloaded.GetGroup(context.Background(), "group-c")
	if err != nil {
		t.Fatal(err)
	}
	if !group.CanRedo {
		t.Fatalf("latest group redo should remain available: %#v", group)
	}
}

func TestGroupProjectionStaysSortedAcrossOperationReplay(t *testing.T) {
	// Operation changes are sorted by path while their sequence numbers follow
	// plan order; the incremental projection must keep group membership ordered
	// by sequence after both live apply and restart replay.
	workspace := t.TempDir()
	service, err := NewService(workspace)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, workspace, "b.md", "b\n")
	writeTestFile(t, workspace, "a.md", "a\n")
	for _, path := range []string{"b.md", "a.md"} {
		if _, err := service.ApplyEdits(context.Background(), ApplyEditsRequest{
			Path:         path,
			BaseRevision: Revision([]byte(path[:1] + "\n")),
			Edits:        []TextEdit{{OldString: path[:1], NewString: path[:1] + "-new"}},
			Metadata:     ChangeMetadata{Origin: OriginAgent, ChangeGroupID: "op-group"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	reloaded, err := NewService(workspace)
	if err != nil {
		t.Fatal(err)
	}
	group, err := reloaded.GetGroup(context.Background(), "op-group")
	if err != nil {
		t.Fatal(err)
	}
	if len(group.ChangeSets) != 2 {
		t.Fatalf("expected two change sets: %#v", group.ChangeSets)
	}
	if group.ChangeSets[0].Sequence > group.ChangeSets[1].Sequence {
		t.Fatalf("group change sets are not sequence-ordered: %#v", group.ChangeSets)
	}
}
