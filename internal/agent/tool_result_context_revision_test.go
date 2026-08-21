package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"denova/config"
)

// readFileExchange builds a retained read_file assistant-call↔result pair whose
// stored tool result carries a full-file revision anchor and a verbatim body,
// mirroring what the recorder now persists at record time.
func readFileExchange(callID, path, revision, body string) []*schema.Message {
	metadata := fmt.Sprintf(`{"schema":"workspace_file.read.v2","file_path":%q,"offset":1,"limit":2000,"revision":%q}`, path, revision)
	return []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: callID, Type: "function",
			Function: schema.FunctionCall{Name: "read_file", Arguments: fmt.Sprintf(`{"file_path":%q}`, path)},
		}}),
		schema.ToolMessage(metadata+"\n     1\t"+body, callID, schema.WithToolName("read_file")),
	}
}

func idePolicy(budget int) ToolResultContextPolicy {
	return ToolResultContextPolicy{AgentKind: config.AgentKindIDE, Enabled: true, RetainedProseMaxBytes: budget}
}

// revisionOnlyResolver keeps the pre-refresh behavior: changed files collapse
// because no window resolver can rebuild their current content.
func revisionOnlyResolver(revisions map[string]string) ToolResultFileResolver {
	return ToolResultFileResolver{
		Revision: func(path string) (string, bool) {
			revision, ok := revisions[path]
			return revision, ok
		},
	}
}

// TestRetentionKeepsBodyWhenRevisionUnchanged is the core guarantee: an unchanged
// file's body stays verbatim in the assembled context, so the writing agent need
// not re-read it and the reusable prefix stays byte-identical.
func TestRetentionKeepsBodyWhenRevisionUnchanged(t *testing.T) {
	messages := readFileExchange("call-1", "/workspace/chapters/ch00001.md", "sha256:same", "上一章结尾正文")
	resolver := revisionOnlyResolver(map[string]string{"/workspace/chapters/ch00001.md": "sha256:same"})

	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(256*1024), resolver)
	if len(filtered) != 2 {
		t.Fatalf("assistant call and result should both remain: %#v", filtered)
	}
	if filtered[1].Content != messages[1].Content {
		t.Fatalf("unchanged file body must be kept byte-identical, got: %s", filtered[1].Content)
	}
}

// TestRetentionRefreshesBodyWhenRevisionChanged is the anti re-read core: a file
// that changed after the read — most commonly through the model's own edits —
// is refreshed to its CURRENT window instead of collapsing, so the next turn
// starts with current line numbers and a current revision.
func TestRetentionRefreshesBodyWhenRevisionChanged(t *testing.T) {
	messages := readFileExchange("call-1", "/workspace/chapters/ch00001.md", "sha256:old", "过期正文")
	resolver := ToolResultFileResolver{
		Revision: func(path string) (string, bool) { return "sha256:new", true },
		Window: func(path string, offset, limit int) (ToolResultFileView, bool) {
			if path != "/workspace/chapters/ch00001.md" || offset != 1 || limit != 2000 {
				return ToolResultFileView{}, false
			}
			return ToolResultFileView{Content: "当前第一行\n当前第二行\n", Revision: "sha256:new"}, true
		},
	}

	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(256*1024), resolver)
	if len(filtered) != 2 {
		t.Fatalf("changed file must keep the pair, not orphan the call: %#v", filtered)
	}
	content := filtered[1].Content
	if strings.Contains(content, "过期正文") {
		t.Fatalf("stale body must not survive the refresh: %s", content)
	}
	if strings.Contains(content, retainedToolReceiptSchema) {
		t.Fatalf("changed file with a window resolver must refresh, not collapse: %s", content)
	}
	for _, want := range []string{`"revision":"sha256:new"`, `"refreshed":true`, "当前第一行", "     1\t", "     2\t"} {
		if !strings.Contains(content, want) {
			t.Fatalf("refreshed body missing %q: %s", want, content)
		}
	}
}

// TestRetentionCollapsesChangedBodyWithoutWindow preserves the pre-refresh
// behavior for assemblies that can only check revisions (no window resolver).
func TestRetentionCollapsesChangedBodyWithoutWindow(t *testing.T) {
	messages := readFileExchange("call-1", "/workspace/chapters/ch00001.md", "sha256:old", "过期正文")
	resolver := revisionOnlyResolver(map[string]string{"/workspace/chapters/ch00001.md": "sha256:new"})

	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(256*1024), resolver)
	if len(filtered) != 2 {
		t.Fatalf("changed file must keep the pair (as a receipt), not orphan the call: %#v", filtered)
	}
	if strings.Contains(filtered[1].Content, "过期正文") {
		t.Fatalf("changed file body must not stay in context: %s", filtered[1].Content)
	}
	for _, want := range []string{retainedToolReceiptSchema, `"path":"/workspace/chapters/ch00001.md"`} {
		if !strings.Contains(filtered[1].Content, want) {
			t.Fatalf("changed file must collapse to a path receipt, missing %q: %s", want, filtered[1].Content)
		}
	}
	if strings.Contains(filtered[1].Content, "Re-read this path") {
		t.Fatalf("receipt must not command a re-read: %s", filtered[1].Content)
	}
}

// TestRetentionFoldsWhenWindowDisappears covers a file that shrank past the
// original offset (or vanished): the refresh cannot select a meaningful window,
// so the body falls back to a receipt instead of an empty body.
func TestRetentionFoldsWhenWindowDisappears(t *testing.T) {
	messages := readFileExchange("call-1", "/workspace/chapters/ch00001.md", "sha256:old", "旧正文")
	resolver := ToolResultFileResolver{
		Revision: func(path string) (string, bool) { return "sha256:new", true },
		Window:   func(path string, offset, limit int) (ToolResultFileView, bool) { return ToolResultFileView{}, false },
	}
	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(256*1024), resolver)
	if len(filtered) != 2 || !strings.Contains(filtered[1].Content, retainedToolReceiptSchema) {
		t.Fatalf("unreadable window must collapse to a receipt: %#v", filtered)
	}
}

// TestRetentionNilResolverAlwaysCollapses preserves the pre-revision behavior for
// the compaction source and non-IDE assembly paths.
func TestRetentionNilResolverAlwaysCollapses(t *testing.T) {
	messages := readFileExchange("call-1", "/workspace/chapters/ch00001.md", "sha256:same", "正文")
	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(256*1024), ToolResultFileResolver{})
	if len(filtered) != 2 || !strings.Contains(filtered[1].Content, retainedToolReceiptSchema) {
		t.Fatalf("nil resolver must always collapse read_file to a receipt: %#v", filtered)
	}
}

// TestRetentionZeroBudgetAlwaysCollapses confirms an explicit 0 budget disables
// prose retention even when a resolver reports the file unchanged.
func TestRetentionZeroBudgetAlwaysCollapses(t *testing.T) {
	messages := readFileExchange("call-1", "/workspace/chapters/ch00001.md", "sha256:same", "正文")
	resolver := revisionOnlyResolver(map[string]string{"/workspace/chapters/ch00001.md": "sha256:same"})
	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(0), resolver)
	if len(filtered) != 2 || !strings.Contains(filtered[1].Content, retainedToolReceiptSchema) {
		t.Fatalf("zero budget must always collapse read_file to a receipt: %#v", filtered)
	}
}

// TestRetentionBudgetKeepsNewestFirst verifies the budget spends on the freshest
// bodies first, collapsing older ones to receipts.
func TestRetentionBudgetKeepsNewestFirst(t *testing.T) {
	older := readFileExchange("call-old", "/workspace/chapters/ch00001.md", "sha256:a", strings.Repeat("旧", 400))
	newer := readFileExchange("call-new", "/workspace/chapters/ch00002.md", "sha256:b", strings.Repeat("新", 400))
	messages := append(append([]*schema.Message{}, older...), newer...)
	resolver := revisionOnlyResolver(map[string]string{
		"/workspace/chapters/ch00001.md": "sha256:a",
		"/workspace/chapters/ch00002.md": "sha256:b",
	})
	// Budget large enough for one body (~1.2KB with the metadata line) but not two.
	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(1500), resolver)
	if len(filtered) != 4 {
		t.Fatalf("all four pairs must survive as body-or-receipt: %#v", filtered)
	}
	// newer (last result) keeps its body; older collapses to a receipt.
	if !strings.Contains(filtered[3].Content, "新") || strings.Contains(filtered[3].Content, retainedToolReceiptSchema) {
		t.Fatalf("newest unchanged body should win the budget: %s", filtered[3].Content)
	}
	if strings.Contains(filtered[1].Content, "旧") || !strings.Contains(filtered[1].Content, retainedToolReceiptSchema) {
		t.Fatalf("older body should collapse once the budget is spent: %s", filtered[1].Content)
	}
}

// TestRetentionMissingAnchorCollapses confirms a body with no revision anchor
// (e.g. the file exceeded the anchor cap at read time) cannot be retained.
func TestRetentionMissingAnchorCollapses(t *testing.T) {
	messages := readFileExchange("call-1", "/workspace/chapters/ch00001.md", "", "正文")
	resolver := revisionOnlyResolver(map[string]string{"/workspace/chapters/ch00001.md": "sha256:any"})
	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(256*1024), resolver)
	if len(filtered) != 2 || !strings.Contains(filtered[1].Content, retainedToolReceiptSchema) {
		t.Fatalf("missing anchor must collapse to a receipt: %#v", filtered)
	}
}

// TestRetentionOldSessionReceiptStaysReceipt confirms histories persisted before
// this change (already storing receipts) are left untouched.
func TestRetentionOldSessionReceiptStaysReceipt(t *testing.T) {
	receipt := retainedFileReadReceipt(`{"schema":"workspace_file.read.v2","file_path":"/workspace/chapters/ch00001.md","offset":1,"limit":2000}` + "\n     1\t正文")
	messages := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call-1", Type: "function",
			Function: schema.FunctionCall{Name: "read_file", Arguments: `{"file_path":"/workspace/chapters/ch00001.md"}`},
		}}),
		schema.ToolMessage(receipt, "call-1", schema.WithToolName("read_file")),
	}
	resolver := revisionOnlyResolver(map[string]string{"/workspace/chapters/ch00001.md": "sha256:same"})
	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(256*1024), resolver)
	if len(filtered) != 2 || filtered[1].Content != receipt {
		t.Fatalf("old-session receipt must stay a receipt unchanged: %#v", filtered)
	}
}
