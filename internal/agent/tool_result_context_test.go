package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"

	"denova/config"
)

func TestApplyToolResultContextPolicyPreservesUnprojectedToolExchangeExactly(t *testing.T) {
	arguments := "  {\"command\":\"printf chapter\",\"selection\":\"" + strings.Repeat("段落", 3000) + "\"}  "
	content := "{\"items\":[" + strings.Repeat("{\"name\":\"条目\"},", 3000) + "\nnot-valid-inner-json"
	messages := []*schema.Message{
		schema.UserMessage("读取资料"),
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call-large", Type: "function",
			Function: schema.FunctionCall{Name: "execute", Arguments: arguments},
		}}),
		schema.ToolMessage(content, "call-large", schema.WithToolName("execute")),
		schema.UserMessage("继续"),
	}

	filtered := applyToolResultContextPolicy(messages, ToolResultContextPolicy{Enabled: true})
	if len(filtered) != len(messages) {
		t.Fatalf("complete tool exchange should remain in context: got=%d want=%d", len(filtered), len(messages))
	}
	if got := filtered[1].ToolCalls[0].Function.Arguments; got != arguments {
		t.Fatalf("tool arguments were rewritten: got_bytes=%d want_bytes=%d", len(got), len(arguments))
	}
	if got := filtered[2].Content; got != content {
		t.Fatalf("tool result was rewritten: got_bytes=%d want_bytes=%d", len(got), len(content))
	}
	if strings.Contains(filtered[2].Content, "tool_result.placeholder") || strings.Contains(filtered[1].ToolCalls[0].Function.Arguments, "args_omitted") {
		t.Fatalf("retained exchanges must not contain synthetic placeholders: %#v", filtered)
	}
}

func TestOpenAIRequestAssemblyKeepsToolContentAsString(t *testing.T) {
	arguments := `{"command":"printf chapter"}`
	content := "{\"items\":[" + strings.Repeat("{\"name\":\"条目\"},", 2000) + "\nnot-valid-inner-json"
	messages := applyToolResultContextPolicy([]*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call-json", Type: "function",
			Function: schema.FunctionCall{Name: "execute", Arguments: arguments},
		}}),
		schema.ToolMessage(content, "call-json", schema.WithToolName("execute")),
		schema.UserMessage("基于结果继续"),
	}, ToolResultContextPolicy{Enabled: true})

	chatModel, err := openai.NewChatModel(context.Background(), &openai.ChatModelConfig{
		APIKey: "test-key",
		Model:  "test-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	stop := errors.New("request captured")
	var rawRequest []byte
	_, err = chatModel.Generate(context.Background(), messages, openai.WithRequestPayloadModifier(
		func(_ context.Context, _ []*schema.Message, rawBody []byte) ([]byte, error) {
			rawRequest = append([]byte(nil), rawBody...)
			return nil, stop
		},
	))
	if !errors.Is(err, stop) {
		t.Fatalf("request capture should stop before network I/O: %v", err)
	}
	if len(rawRequest) == 0 {
		t.Fatalf("OpenAI adapter did not assemble a request: %v", err)
	}

	var request struct {
		Messages []struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				Function struct {
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(rawRequest, &request); err != nil {
		t.Fatalf("decode assembled request: %v\n%s", err, rawRequest)
	}
	if len(request.Messages) != 3 {
		t.Fatalf("assembled messages = %#v", request.Messages)
	}
	if got := request.Messages[0].ToolCalls[0].Function.Arguments; got != arguments {
		t.Fatalf("assembled arguments changed: got=%q want=%q", got, arguments)
	}
	toolMessage := request.Messages[1]
	if toolMessage.Role != "tool" || toolMessage.ToolCallID != "call-json" || toolMessage.Content != content {
		t.Fatalf("tool content must be one opaque JSON string, got role=%q id=%q bytes=%d", toolMessage.Role, toolMessage.ToolCallID, len(toolMessage.Content))
	}
}

func TestApplyToolResultContextPolicyDisabledRemovesToolContext(t *testing.T) {
	messages := []*schema.Message{
		schema.UserMessage("查资料"),
		schema.AssistantMessage("", []schema.ToolCall{{ID: "call-1", Type: "function", Function: schema.FunctionCall{Name: "read_file", Arguments: `{}`}}}),
		schema.ToolMessage("result", "call-1", schema.WithToolName("read_file")),
		schema.AssistantMessage("完成", nil),
	}

	filtered := applyToolResultContextPolicy(messages, ToolResultContextPolicy{Enabled: false})
	if len(filtered) != 2 || filtered[0].Role != schema.User || filtered[1].Content != "完成" {
		t.Fatalf("disabled retention should remove context-only tool messages: %#v", filtered)
	}
}

func TestToolResultContextRecorderPersistsAlreadyBoundedResultExactly(t *testing.T) {
	conversation := &recordedToolContextConversation{policy: ToolResultContextPolicy{Enabled: true, MaxResultBytes: 256}}
	recorder := newToolResultContextRecorder(conversation)
	arguments := `{"command":"printf chapter"}`
	recorder.RecordAssistantToolCalls(schema.AssistantMessage("", []schema.ToolCall{{
		ID: "call-1", Type: "function", Function: schema.FunctionCall{Name: "execute", Arguments: arguments},
	}}), agentEventMetadata{})
	bounded := FilterToolResultForModelWithLimit("execute", arguments, strings.Repeat("正文", 500), 256)
	recorder.RecordToolResult("execute", "call-1", bounded.Content, agentEventMetadata{})

	if len(conversation.messages) != 2 {
		t.Fatalf("recorded messages = %#v", conversation.messages)
	}
	if got := conversation.messages[0].ToolCalls[0].Function.Arguments; got != arguments {
		t.Fatalf("recorded arguments changed: got=%q want=%q", got, arguments)
	}
	if got := conversation.messages[1].Content; got != bounded.Content {
		t.Fatalf("bounded result was filtered a second time: got_bytes=%d want_bytes=%d", len(got), len(bounded.Content))
	}
	if !strings.Contains(conversation.messages[1].Content, "[tool result truncated]") || !strings.Contains(conversation.messages[1].Content, toolResultMetadataHeader) {
		t.Fatalf("tool-boundary truncation metadata should remain intact: %q", conversation.messages[1].Content)
	}
}

func TestToolResultContextRecorderPersistsFileReadReceipt(t *testing.T) {
	conversation := &recordedToolContextConversation{policy: ToolResultContextPolicy{
		AgentKind:      config.AgentKindIDE,
		Enabled:        true,
		MaxResultBytes: 128 * 1024,
	}}
	recorder := newToolResultContextRecorder(conversation)
	arguments := `{"file_path":"/workspace/chapters/ch00001.md","offset":21,"limit":40}`
	recorder.RecordAssistantToolCalls(schema.AssistantMessage("", []schema.ToolCall{{
		ID: "call-read", Type: "function", Function: schema.FunctionCall{Name: "read_file", Arguments: arguments},
	}}), agentEventMetadata{})
	raw := `{"schema":"workspace_file.read.v2","file_path":"/workspace/chapters/ch00001.md","offset":21,"limit":40}` +
		"\n    21\t第一段正文\n    22\t第二段正文"
	bounded := FilterToolResultForModelWithLimit("read_file", arguments, raw, 128*1024)
	recorder.RecordToolResult("read_file", "call-read", bounded.Content, agentEventMetadata{})

	if len(conversation.messages) != 2 {
		t.Fatalf("recorded messages = %#v", conversation.messages)
	}
	receipt := conversation.messages[1].Content
	for _, want := range []string{
		retainedToolReceiptSchema,
		`"tool_name":"read_file"`,
		`"path":"/workspace/chapters/ch00001.md"`,
		`"offset":21`,
		`"limit":40`,
	} {
		if !strings.Contains(receipt, want) {
			t.Fatalf("retained file receipt missing %q: %s", want, receipt)
		}
	}
	if strings.Contains(receipt, "第一段正文") || strings.Contains(receipt, "第二段正文") {
		t.Fatalf("file body must not be duplicated into the next-turn context: %s", receipt)
	}
}

func TestToolResultContextRecorderProjectsSuccessfulWorkspaceWriteArgumentsAtAssembly(t *testing.T) {
	conversation := &recordedToolContextConversation{policy: ToolResultContextPolicy{
		AgentKind: config.AgentKindIDE,
		Enabled:   true,
	}}
	recorder := newToolResultContextRecorder(conversation)
	arguments := `{"file_path":"/workspace/chapters/ch00001.md","content":"` + strings.Repeat("章节正文", 2000) + `"}`
	recorder.RecordAssistantToolCalls(schema.AssistantMessage("", []schema.ToolCall{{
		ID: "call-write", Type: "function", Function: schema.FunctionCall{Name: "write_file", Arguments: arguments},
	}}), agentEventMetadata{})
	result := `{"schema":"workspace_change.tool_result.v1","status":"applied","workspace":"/workspace","change_group_id":"group-1","change_set_id":"change-1","path":"chapters/ch00001.md","review_status":"pending","apply_state":"applied"}`
	recorder.RecordToolResult("write_file", "call-write", result, agentEventMetadata{})

	if len(conversation.messages) != 2 {
		t.Fatalf("recorded messages = %#v", conversation.messages)
	}
	if got := conversation.messages[0].ToolCalls[0].Function.Arguments; got != arguments {
		t.Fatalf("durable tool context should preserve original arguments until the result can confirm success: got_bytes=%d want_bytes=%d", len(got), len(arguments))
	}
	modelMessages := applyToolResultContextPolicy(conversation.messages, conversation.policy)
	if len(modelMessages) != 2 {
		t.Fatalf("model messages = %#v", modelMessages)
	}
	projected := modelMessages[0].ToolCalls[0].Function.Arguments
	for _, want := range []string{
		retainedToolCallSchema,
		`"tool_name":"write_file"`,
		`"path":"/workspace/chapters/ch00001.md"`,
		`"content_omitted":true`,
	} {
		if !strings.Contains(projected, want) {
			t.Fatalf("projected write arguments missing %q: %s", want, projected)
		}
	}
	if strings.Contains(projected, "章节正文") {
		t.Fatalf("written body must not be duplicated into next-turn tool arguments: %s", projected)
	}
	if !strings.Contains(modelMessages[1].Content, `"change_set_id":"change-1"`) {
		t.Fatalf("workspace change receipt should remain available: %s", modelMessages[1].Content)
	}
}

func TestApplyToolResultContextPolicyKeepsFailedWriteArguments(t *testing.T) {
	arguments := `{"file_path":"/workspace/chapters/ch00001.md","content":"` + strings.Repeat("待写正文", 1000) + `"}`
	failure := `[tool error] stale workspace revision`
	messages := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call-write", Type: "function", Function: schema.FunctionCall{Name: "write_file", Arguments: arguments},
		}}),
		schema.ToolMessage(failure, "call-write", schema.WithToolName("write_file")),
		schema.UserMessage("继续"),
	}
	filtered := applyToolResultContextPolicy(messages, ToolResultContextPolicy{AgentKind: config.AgentKindIDE, Enabled: true})
	if len(filtered) != 3 {
		t.Fatalf("failed write exchange should remain paired: %#v", filtered)
	}
	if got := filtered[0].ToolCalls[0].Function.Arguments; got != arguments {
		t.Fatalf("failed write must keep retry evidence: got_bytes=%d want_bytes=%d", len(got), len(arguments))
	}
	if filtered[1].Content != failure {
		t.Fatalf("failed write result changed: %q", filtered[1].Content)
	}
}

func TestApplyToolResultContextPolicyKeepsPendingWriteArguments(t *testing.T) {
	arguments := `{"file_path":"/workspace/chapters/ch00001.md","content":"` + strings.Repeat("待审核正文", 1000) + `"}`
	pending := `{"schema":"workspace_change.tool_result.v1","status":"pending","workspace":"/workspace","change_group_id":"group-1","change_set_id":"change-1","path":"chapters/ch00001.md","review_status":"pending","apply_state":"pending"}`
	messages := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call-write", Type: "function", Function: schema.FunctionCall{Name: "write_file", Arguments: arguments},
		}}),
		schema.ToolMessage(pending, "call-write", schema.WithToolName("write_file")),
		schema.UserMessage("继续"),
	}
	filtered := applyToolResultContextPolicy(messages, ToolResultContextPolicy{AgentKind: config.AgentKindIDE, Enabled: true})
	if len(filtered) != 3 {
		t.Fatalf("pending write exchange should remain paired: %#v", filtered)
	}
	if got := filtered[0].ToolCalls[0].Function.Arguments; got != arguments {
		t.Fatalf("pending write must keep content until it is applied: got_bytes=%d want_bytes=%d", len(got), len(arguments))
	}
}

func TestToolResultContextRecorderSkipsMalformedCallAndResult(t *testing.T) {
	conversation := &recordedToolContextConversation{policy: ToolResultContextPolicy{Enabled: true}}
	recorder := newToolResultContextRecorder(conversation)
	recorder.RecordAssistantToolCalls(schema.AssistantMessage("", []schema.ToolCall{{
		ID: "call-invalid", Type: "function",
		Function: schema.FunctionCall{Name: "write_file", Arguments: `{"content":`},
	}}), agentEventMetadata{})
	recorder.RecordToolResult("write_file", "call-invalid", "invalid arguments", agentEventMetadata{})
	if len(conversation.messages) != 0 {
		t.Fatalf("malformed tool call and result must not persist: %#v", conversation.messages)
	}
}

func TestApplyToolResultContextPolicyDropsMalformedAndOrphanedPairs(t *testing.T) {
	messages := []*schema.Message{
		schema.AssistantMessage("useful narration", []schema.ToolCall{
			{ID: "invalid", Type: "function", Function: schema.FunctionCall{Name: "read_file", Arguments: `{"path":`}},
			{ID: "missing", Type: "function", Function: schema.FunctionCall{Name: "read_file", Arguments: `{}`}},
		}),
		schema.ToolMessage("invalid arguments", "invalid", schema.WithToolName("read_file")),
		schema.ToolMessage("orphan result", "unknown", schema.WithToolName("read_file")),
		schema.UserMessage("继续"),
	}
	filtered := applyToolResultContextPolicy(messages, ToolResultContextPolicy{Enabled: true})
	if len(filtered) != 2 || filtered[0].Content != "useful narration" || len(filtered[0].ToolCalls) != 0 || filtered[1].Role != schema.User {
		t.Fatalf("invalid protocol messages must not enter the next request: %#v", filtered)
	}
}

func TestApplyToolResultContextPolicyDropsTransientIndexesWithTheirCalls(t *testing.T) {
	messages := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{ID: "call-list", Type: "function", Function: schema.FunctionCall{Name: "list_lore_items", Arguments: `{"keywords":["门"]}`}}}),
		schema.ToolMessage("很长的资料索引", "call-list", schema.WithToolName("list_lore_items")),
		schema.AssistantMessage("继续故事", nil),
	}
	filtered := applyToolResultContextPolicy(messages, ToolResultContextPolicy{Enabled: true})
	if len(filtered) != 1 || filtered[0].Content != "继续故事" {
		t.Fatalf("transient index call and result should not cross turns: %#v", filtered)
	}
}

func TestApplyToolResultContextPolicyDropsTransientSearchAndTodoTools(t *testing.T) {
	messages := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{
			{ID: "call-grep", Type: "function", Function: schema.FunctionCall{Name: "grep", Arguments: `{"pattern":"禁用句式","path":"chapters"}`}},
			{ID: "call-todo", Type: "function", Function: schema.FunctionCall{Name: "write_todos", Arguments: `{"todos":[{"content":"检查正文","status":"completed"}]}`}},
		}),
		schema.ToolMessage("chapters/ch00001.md:12:禁用句式", "call-grep", schema.WithToolName("grep")),
		schema.ToolMessage("Updated todo list", "call-todo", schema.WithToolName("write_todos")),
		schema.AssistantMessage("检查完成，正文已处理。", nil),
	}
	filtered := applyToolResultContextPolicy(messages, ToolResultContextPolicy{AgentKind: config.AgentKindIDE, Enabled: true})
	if len(filtered) != 1 || filtered[0].Content != "检查完成，正文已处理。" {
		t.Fatalf("search snapshots and run-local todos must not cross turns: %#v", filtered)
	}
}

func TestApplyToolResultContextPolicyProjectsHistoricalEditArguments(t *testing.T) {
	arguments := `{"file_path":"/workspace/chapters/ch00001.md","edits":[` +
		`{"old_string":"` + strings.Repeat("旧正文", 1000) + `","new_string":"` + strings.Repeat("新正文", 1000) + `"},` +
		`{"old_string":"旧句","new_string":"新句"}]}`
	messages := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call-edit", Type: "function", Function: schema.FunctionCall{Name: "edit_file", Arguments: arguments},
		}}),
		schema.ToolMessage(`{"schema":"workspace_change.tool_result.v1","status":"applied","workspace":"/workspace","change_group_id":"group-1","change_set_id":"change-1","path":"chapters/ch00001.md","review_status":"pending","apply_state":"applied"}`, "call-edit", schema.WithToolName("edit_file")),
		schema.UserMessage("继续"),
	}
	filtered := applyToolResultContextPolicy(messages, ToolResultContextPolicy{AgentKind: config.AgentKindIDE, Enabled: true})
	if len(filtered) != 3 {
		t.Fatalf("projected edit exchange should remain paired: %#v", filtered)
	}
	projected := filtered[0].ToolCalls[0].Function.Arguments
	for _, want := range []string{
		retainedToolCallSchema,
		`"tool_name":"edit_file"`,
		`"path":"/workspace/chapters/ch00001.md"`,
		`"operation_count":2`,
		`"content_omitted":true`,
	} {
		if !strings.Contains(projected, want) {
			t.Fatalf("projected edit arguments missing %q: %s", want, projected)
		}
	}
	if strings.Contains(projected, "旧正文") || strings.Contains(projected, "新正文") {
		t.Fatalf("historical patch bodies must not remain in next-turn arguments: %s", projected)
	}
}

func TestApplyToolResultContextPolicyProjectsHistoricalLoreWriteArguments(t *testing.T) {
	arguments := `{"message":"同步长期设定","items":[` +
		`{"id":"hero","name":"主角","content":"` + strings.Repeat("长期设定", 1000) + `"},` +
		`{"id":"town","name":"城镇","content":"地点设定"}],"delete_ids":["retired-item"]}`
	messages := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{{
			ID: "call-lore-write", Type: "function", Function: schema.FunctionCall{Name: "write_lore_items", Arguments: arguments},
		}}),
		schema.ToolMessage("同步长期设定（更新 2，删除 1）\nitem_ids: [\"hero\",\"town\"]\ndeleted_ids: [\"retired-item\"]", "call-lore-write", schema.WithToolName("write_lore_items")),
		schema.UserMessage("继续"),
	}
	filtered := applyToolResultContextPolicy(messages, ToolResultContextPolicy{AgentKind: config.AgentKindIDE, Enabled: true})
	if len(filtered) != 3 {
		t.Fatalf("projected Lore write exchange should remain paired: %#v", filtered)
	}
	projected := filtered[0].ToolCalls[0].Function.Arguments
	for _, want := range []string{
		retainedToolCallSchema,
		`"tool_name":"write_lore_items"`,
		`"operation_count":3`,
		`"content_omitted":true`,
	} {
		if !strings.Contains(projected, want) {
			t.Fatalf("projected Lore write arguments missing %q: %s", want, projected)
		}
	}
	if strings.Contains(projected, "长期设定") || strings.Contains(projected, "地点设定") {
		t.Fatalf("historical Lore bodies must not remain in next-turn arguments: %s", projected)
	}
}

func TestToolResultContextReplacesLoreBodiesWithSourceReceipt(t *testing.T) {
	raw := "# 资料库条目\n\n## 黄泉酒馆（location / major / resident）\nID：lore-tavern\n\n```markdown\n掌柜隐藏着不可公开的秘密正文。\n```"
	content := toolResultContextContent("read_lore_items", raw, ToolResultContextPolicy{})
	for _, want := range []string{retainedToolReceiptSchema, "read_lore_items", "lore-tavern", "黄泉酒馆"} {
		if !strings.Contains(content, want) {
			t.Fatalf("retained lore receipt missing %q: %s", want, content)
		}
	}
	if strings.Contains(content, "不可公开的秘密正文") {
		t.Fatalf("lore body must not be duplicated into cross-turn context: %s", content)
	}
}

func TestToolResultContextReplacesSkillBodyWithSourceReceipt(t *testing.T) {
	raw := "Launching skill: novel-standard\n" +
		"Base directory for this skill: /workspace/skills/novel-standard\n\n" +
		"# novel-standard\n\n完整写作流程与大量方法正文。"
	content := toolResultContextContent("skill", raw, ToolResultContextPolicy{AgentKind: config.AgentKindIDE})
	for _, want := range []string{
		retainedToolReceiptSchema,
		`"tool_name":"skill"`,
		`"names":["novel-standard"]`,
		`"path":"/workspace/skills/novel-standard"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("retained skill receipt missing %q: %s", want, content)
		}
	}
	if strings.Contains(content, "完整写作流程") || strings.Contains(content, "大量方法正文") {
		t.Fatalf("skill body must not be duplicated into cross-turn context: %s", content)
	}
}

func TestToolResultContextReplacesFileBodiesWithSourceReceipt(t *testing.T) {
	raw := `{"schema":"workspace_file.read.v2","file_path":"/workspace/chapters/ch00001.md","offset":21,"limit":40}` +
		"\n    21\t第一段正文\n    22\t第二段正文"
	content := toolResultContextContent("read_file", raw, ToolResultContextPolicy{AgentKind: config.AgentKindIDE})
	for _, want := range []string{
		retainedToolReceiptSchema,
		`"tool_name":"read_file"`,
		`"path":"/workspace/chapters/ch00001.md"`,
		`"offset":21`,
		`"limit":40`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("retained file receipt missing %q: %s", want, content)
		}
	}
	if strings.Contains(content, "第一段正文") || strings.Contains(content, "第二段正文") {
		t.Fatalf("file body must not be duplicated into cross-turn context: %s", content)
	}
}

func TestInteractiveStoryToolContextKeepsOnlySemanticReadReceipts(t *testing.T) {
	messages := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{
			{ID: "prepare", Type: "function", Function: schema.FunctionCall{Name: "prepare_interactive_turn", Arguments: `{}`}},
			{ID: "file", Type: "function", Function: schema.FunctionCall{Name: "read_file", Arguments: `{}`}},
			{ID: "lore", Type: "function", Function: schema.FunctionCall{Name: "read_lore_items", Arguments: `{}`}},
		}),
		schema.ToolMessage(`{"outcome":"success"}`, "prepare", schema.WithToolName("prepare_interactive_turn")),
		schema.ToolMessage("文风正文", "file", schema.WithToolName("read_file")),
		schema.ToolMessage("# 资料库条目\n\n## 酒馆\nID：lore-tavern\n\n秘密正文", "lore", schema.WithToolName("read_lore_items")),
	}
	filtered := applyToolResultContextPolicy(messages, ToolResultContextPolicy{AgentKind: config.AgentKindInteractiveStory, Enabled: true})
	if len(filtered) != 2 || len(filtered[0].ToolCalls) != 1 || filtered[0].ToolCalls[0].Function.Name != "read_lore_items" || !strings.Contains(filtered[1].Content, retainedToolReceiptSchema) {
		t.Fatalf("game context should contain only the semantic lore receipt pair: %#v", filtered)
	}
}

func TestApplyToolResultContextPolicyPairsByCallIDWhenResultToolNameMissing(t *testing.T) {
	messages := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{
			{ID: "call-list", Type: "function", Function: schema.FunctionCall{Name: "list_lore_items", Arguments: `{}`}},
			{ID: "call-read", Type: "function", Function: schema.FunctionCall{Name: "read_lore_items", Arguments: `{}`}},
		}),
		schema.ToolMessage("索引结果", "call-list"),
		schema.ToolMessage("# 资料库条目\n\n## 酒馆\nID：lore-tavern\n\n秘密正文", "call-read"),
	}
	filtered := applyToolResultContextPolicy(messages, ToolResultContextPolicy{Enabled: true})
	if len(filtered) != 2 || len(filtered[0].ToolCalls) != 1 || filtered[0].ToolCalls[0].ID != "call-read" {
		t.Fatalf("only the retained call/result pair should remain: %#v", filtered)
	}
	if filtered[1].ToolName != "read_lore_items" || !strings.Contains(filtered[1].Content, retainedToolReceiptSchema) {
		t.Fatalf("result should inherit its paired tool name and become a receipt: %#v", filtered[1])
	}
}

func TestApplyToolResultContextPolicyDropsAmbiguousDuplicatePair(t *testing.T) {
	messages := []*schema.Message{
		schema.AssistantMessage("", []schema.ToolCall{
			{ID: "duplicate", Type: "function", Function: schema.FunctionCall{Name: "read_file", Arguments: `{}`}},
			{ID: "duplicate", Type: "function", Function: schema.FunctionCall{Name: "read_lore_items", Arguments: `{}`}},
		}),
		schema.ToolMessage("ambiguous", "duplicate"),
	}
	if filtered := applyToolResultContextPolicy(messages, ToolResultContextPolicy{Enabled: true}); len(filtered) != 0 {
		t.Fatalf("duplicate call ids must be dropped instead of mispaired: %#v", filtered)
	}
}

func TestToolResultContextKeepsLoreErrorsInsteadOfPositiveReceipt(t *testing.T) {
	raw := "读取资料失败：条目不存在"
	if content := toolResultContextContent("read_lore_items", raw, ToolResultContextPolicy{}); content != raw {
		t.Fatalf("failed reads should remain errors instead of positive receipts: %q", content)
	}
}

func TestToolResultContextKeepsFileErrorsInsteadOfPositiveReceipt(t *testing.T) {
	raw := "[tool error] file not found: /workspace/chapters/missing.md"
	if content := toolResultContextContent("read_file", raw, ToolResultContextPolicy{AgentKind: config.AgentKindIDE}); content != raw {
		t.Fatalf("failed reads should remain errors instead of positive receipts: %q", content)
	}
}

func TestToolResultContextKeepsSkillErrorsInsteadOfPositiveReceipt(t *testing.T) {
	raw := "[tool error] skill not found: missing-skill"
	if content := toolResultContextContent("skill", raw, ToolResultContextPolicy{AgentKind: config.AgentKindIDE}); content != raw {
		t.Fatalf("failed skill loads should remain errors instead of positive receipts: %q", content)
	}
}

type recordedToolContextConversation struct {
	Conversation
	messages []*schema.Message
	policy   ToolResultContextPolicy
}

func (c *recordedToolContextConversation) AppendContextMessage(msg *schema.Message) error {
	c.messages = append(c.messages, msg)
	return nil
}

func (c *recordedToolContextConversation) ToolResultContextPolicy() ToolResultContextPolicy {
	return c.policy
}
