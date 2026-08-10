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

// TestRetentionKeepsBodyWhenRevisionUnchanged is the core guarantee: an unchanged
// file's body stays verbatim in the assembled context, so the writing agent need
// not re-read it and the reusable prefix stays byte-identical.
func TestRetentionKeepsBodyWhenRevisionUnchanged(t *testing.T) {
	messages := readFileExchange("call-1", "/workspace/chapters/ch00001.md", "sha256:same", "上一章结尾正文")
	resolver := func(path string) (string, bool) { return "sha256:same", true }

	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(256*1024), resolver)
	if len(filtered) != 2 {
		t.Fatalf("assistant call and result should both remain: %#v", filtered)
	}
	if !strings.Contains(filtered[1].Content, "上一章结尾正文") {
		t.Fatalf("unchanged file body must be kept verbatim: %s", filtered[1].Content)
	}
	if strings.Contains(filtered[1].Content, retainedToolReceiptSchema) {
		t.Fatalf("unchanged file must not collapse to a receipt: %s", filtered[1].Content)
	}
}

// TestRetentionCollapsesBodyWhenRevisionChanged confirms a stale body is dropped
// to a receipt that names the path so the agent performs a targeted re-read.
func TestRetentionCollapsesBodyWhenRevisionChanged(t *testing.T) {
	messages := readFileExchange("call-1", "/workspace/chapters/ch00001.md", "sha256:old", "过期正文")
	resolver := func(path string) (string, bool) { return "sha256:new", true }

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
}

// TestRetentionNilResolverAlwaysCollapses preserves the pre-revision behavior for
// the compaction source and non-IDE assembly paths.
func TestRetentionNilResolverAlwaysCollapses(t *testing.T) {
	messages := readFileExchange("call-1", "/workspace/chapters/ch00001.md", "sha256:same", "正文")
	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(256*1024), nil)
	if len(filtered) != 2 || !strings.Contains(filtered[1].Content, retainedToolReceiptSchema) {
		t.Fatalf("nil resolver must always collapse read_file to a receipt: %#v", filtered)
	}
}

// TestRetentionZeroBudgetAlwaysCollapses confirms an explicit 0 budget disables
// prose retention even when a resolver reports the file unchanged.
func TestRetentionZeroBudgetAlwaysCollapses(t *testing.T) {
	messages := readFileExchange("call-1", "/workspace/chapters/ch00001.md", "sha256:same", "正文")
	resolver := func(path string) (string, bool) { return "sha256:same", true }
	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(0), resolver)
	if len(filtered) != 2 || !strings.Contains(filtered[1].Content, retainedToolReceiptSchema) {
		t.Fatalf("zero budget must always collapse read_file to a receipt: %#v", filtered)
	}
}

// TestRetentionBudgetKeepsNewestFirst verifies the budget spends on the freshest
// unchanged bodies first, collapsing older ones to receipts.
func TestRetentionBudgetKeepsNewestFirst(t *testing.T) {
	older := readFileExchange("call-old", "/workspace/chapters/ch00001.md", "sha256:a", strings.Repeat("旧", 400))
	newer := readFileExchange("call-new", "/workspace/chapters/ch00002.md", "sha256:b", strings.Repeat("新", 400))
	messages := append(append([]*schema.Message{}, older...), newer...)
	resolver := func(path string) (string, bool) {
		switch path {
		case "/workspace/chapters/ch00001.md":
			return "sha256:a", true
		case "/workspace/chapters/ch00002.md":
			return "sha256:b", true
		}
		return "", false
	}
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
	resolver := func(path string) (string, bool) { return "sha256:any", true }
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
	resolver := func(path string) (string, bool) { return "sha256:same", true }
	filtered := applyToolResultContextPolicyWithResolver(messages, idePolicy(256*1024), resolver)
	if len(filtered) != 2 || filtered[1].Content != receipt {
		t.Fatalf("old-session receipt must stay a receipt unchanged: %#v", filtered)
	}
}
