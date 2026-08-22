package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"denova/config"
	agentcontext "denova/internal/agent/context"
	"denova/internal/session"
)

func TestSessionConversationKeepsFullEffectiveHistoryBeforeCompaction(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 4; i++ {
		if err := sess.Append(schema.UserMessage("user " + string(rune('0'+i)))); err != nil {
			t.Fatal(err)
		}
		if err := sess.Append(schema.AssistantMessage("assistant "+string(rune('0'+i)), nil)); err != nil {
			t.Fatal(err)
		}
	}
	conversation := NewSessionConversation(sess)
	history, err := conversation.PrepareMessages("user 5", "agent user 5")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 9 {
		t.Fatalf("history length = %d, want 9", len(history))
	}
	want := []string{
		"user 1", "assistant 1",
		"user 2", "assistant 2",
		"user 3", "assistant 3",
		"user 4", "assistant 4",
		"agent user 5",
	}
	for i := range want {
		if history[i].Content != want[i] {
			t.Fatalf("history[%d] = %q, want %q; all=%#v", i, history[i].Content, want[i], history)
		}
	}
}

func TestSessionConversationPersistsUserMessageReferencesOutsideModelContent(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	conversation := NewSessionConversation(sess)
	conversation.SetUserMessageReferences([]session.UserMessageReference{
		{Kind: "file", Label: "chapters/ch01.md"},
		{Kind: "review_comment", ID: "comment-1", Label: "setting/test.md", Detail: "需要增加爽点"},
	})

	history, err := conversation.PrepareMessages("请统一修改", "请统一修改")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].Content != "请统一修改" {
		t.Fatalf("display references must not be injected into model content: %#v", history)
	}
	visible := sess.History()
	if len(visible) != 1 || len(visible[0].UserReferences) != 2 {
		t.Fatalf("user references were not persisted: %#v", visible)
	}
	if visible[0].UserReferences[1].Detail != "需要增加爽点" {
		t.Fatalf("review comment display detail was lost: %#v", visible[0].UserReferences)
	}
}

func TestSessionConversationPrependsDynamicContextInsideFinalUserMessageOnly(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.UserMessage("旧用户请求")); err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.AssistantMessage("旧助手回复", nil)); err != nil {
		t.Fatal(err)
	}

	conversation := NewSessionConversationForAgentWithRuntimeContext(
		sess,
		&config.Config{},
		config.AgentKindIDE,
		"本轮动态作品状态",
		"## 大纲\n\n主角进入废城。",
	)
	history, err := conversation.PrepareMessages("继续写", "继续写")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("history length = %d, want 3: %#v", len(history), history)
	}
	final := history[len(history)-1].Content
	dynamicIndex := strings.Index(final, "# 本轮动态作品状态")
	requestIndex := strings.Index(final, "# 本轮用户请求（最高优先级）")
	if dynamicIndex < 0 || requestIndex < 0 || dynamicIndex >= requestIndex {
		t.Fatalf("final model message should place dynamic context before the current request:\n%s", final)
	}
	if !strings.Contains(final, "主角进入废城") || !strings.HasSuffix(strings.TrimSpace(final), "继续写") {
		t.Fatalf("final model message missing dynamic state or bottom request:\n%s", final)
	}
	visible := sess.History()
	if got := visible[len(visible)-1].Content; got != "继续写" {
		t.Fatalf("visible session history should keep original user message, got %q", got)
	}
	if sources := conversation.ContextSourceSummary(); !strings.Contains(sources, "本轮动态上下文") || !strings.Contains(sources, "prepended_to_final_user_message") {
		t.Fatalf("runtime context source summary missing dynamic context: %s", sources)
	}
}

func TestSessionConversationPlacesWorkspaceContextsAfterHistory(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.UserMessage("旧用户请求")); err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.AssistantMessage("旧助手回复", nil)); err != nil {
		t.Fatal(err)
	}

	conversation := NewSessionConversationForAgentWithRuntimeContexts(
		sess,
		&config.Config{},
		config.AgentKindIDE,
		"稳定作品上下文",
		"## 当前大纲\n\n主角进入废城。",
		"本轮动态作品状态",
		"## 当前进度\n\n刚抵达废城。",
	)
	history, err := conversation.PrepareMessages("继续写", "继续写")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("history length = %d, want 3: %#v", len(history), history)
	}
	if history[0].Content != "旧用户请求" || history[1].Content != "旧助手回复" {
		t.Fatalf("persisted history should remain the reusable model prefix: %#v", messageContents(history))
	}
	final := history[2].Content
	stableIndex := strings.Index(final, "# 稳定作品上下文")
	dynamicIndex := strings.Index(final, "# 本轮动态作品状态")
	requestIndex := strings.Index(final, "# 本轮用户请求（最高优先级）")
	if stableIndex < 0 || dynamicIndex < 0 || requestIndex < 0 || stableIndex >= dynamicIndex || dynamicIndex >= requestIndex {
		t.Fatalf("final model message should contain stable state, dynamic state, then the request:\n%s", final)
	}
	if !strings.Contains(final, "主角进入废城") || !strings.Contains(final, "刚抵达废城") || !strings.HasSuffix(strings.TrimSpace(final), "继续写") {
		t.Fatalf("final model message is missing workspace context or request:\n%s", final)
	}
	if visible := sess.History(); len(visible) != 3 || visible[2].Content != "继续写" {
		t.Fatalf("visible session history should only include raw user request: %#v", visible)
	}
	if sources := conversation.ContextSourceSummary(); strings.Contains(sources, "prepended_to_model_messages") || strings.Count(sources, "prepended_to_final_user_message") != 2 {
		t.Fatalf("both workspace contexts should be placed after reusable history: %s", sources)
	}
}

func TestSessionConversationKeepsRawRequestAtAbsoluteTailAfterWorkspaceContext(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.UserMessage("上一轮请求")); err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.AssistantMessage("上一轮回答", nil)); err != nil {
		t.Fatal(err)
	}

	composition := composeAgentInput(ChatRequest{
		Message:      "继续下一章",
		WritingSkill: "novel-standard",
		LoadedWritingSkill: &LoadedWritingSkill{
			Name:          "novel-standard",
			Content:       "# novel-standard\n\n执行正式写作流程",
			BaseDirectory: "/app/skills/novel-standard",
		},
	}, nil, nil, DefaultLoopPolicy())
	conversation := NewSessionConversationForAgentWithRuntimeContexts(
		sess,
		&config.Config{},
		config.AgentKindIDE,
		"当前作品基础上下文",
		"## 当前大纲\n\n大纲内容",
		"当前作品动态上下文",
		"## 当前进度\n\n进度内容",
	)
	messages, err := conversation.PrepareMessages(composition.OriginalMessage, composition.AgentMessage)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("messages length = %d, want 3: %#v", len(messages), messages)
	}
	final := messages[len(messages)-1].Content
	for _, required := range []string{
		"# 当前作品基础上下文",
		"# 当前作品动态上下文",
		"# 本轮规则、附件与请求",
		"# 已加载的内置 Writing Skill",
		"[上下文边界]",
		"# 本轮用户请求（最高优先级）",
	} {
		if !strings.Contains(final, required) {
			t.Fatalf("final model message missing %q:\n%s", required, final)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(final), "继续下一章") {
		t.Fatalf("raw request must be the absolute tail of the final model message:\n%s", final)
	}
	if strings.Count(final, "继续下一章") != 1 {
		t.Fatalf("raw request should appear exactly once, got %d:\n%s", strings.Count(final, "继续下一章"), final)
	}
	if strings.Count(final, "# 本轮用户请求（最高优先级）") != 1 {
		t.Fatalf("authoritative request heading should appear exactly once:\n%s", final)
	}
}

func TestSessionConversationWorkspaceEditsPreserveHistoryPrefix(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.UserMessage("上一轮讨论")); err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.AssistantMessage("上一轮结论", nil)); err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.UserMessage("当前请求占位")); err != nil {
		t.Fatal(err)
	}

	build := func(stable, dynamic string) []*schema.Message {
		conversation := NewSessionConversationForAgentWithRuntimeContexts(
			sess,
			&config.Config{},
			config.AgentKindIDE,
			"稳定作品上下文",
			stable,
			"本轮动态作品状态",
			dynamic,
		)
		// modelMessages replaces the already-persisted current user message, so
		// this test can compare two assemblies without appending duplicate turns.
		result, err := agentcontext.Build(context.Background(), agentcontext.Request{
			Messages: conversation.modelMessages("继续写"),
			Sources:  conversation.runtimeContextSources(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return result.Messages
	}

	before := build("旧大纲", "旧进度")
	after := build("新大纲", "新进度")
	if len(before) != 3 || len(after) != 3 {
		t.Fatalf("unexpected model messages: before=%#v after=%#v", before, after)
	}
	for i := 0; i < len(before)-1; i++ {
		if before[i].Role != after[i].Role || before[i].Content != after[i].Content {
			t.Fatalf("workspace edits invalidated reusable history message %d: before=%#v after=%#v", i, before[i], after[i])
		}
	}
	if before[len(before)-1].Content == after[len(after)-1].Content {
		t.Fatalf("current workspace context should change only the final user message")
	}
	if !strings.Contains(after[len(after)-1].Content, "新大纲") || !strings.Contains(after[len(after)-1].Content, "新进度") {
		t.Fatalf("updated workspace context missing from final message: %s", after[len(after)-1].Content)
	}
}

func TestSessionConversationKeepsWorkspaceContextsInRetainedCurrentTurnAfterCompaction(t *testing.T) {
	previous := summarizeContextForCompaction
	defer func() { summarizeContextForCompaction = previous }()
	summarizeContextForCompaction = func(_ context.Context, _ *config.Config, _ string, _ string, _ []*schema.Message, _ string, _ int, _ contextCompactionPolicy, _ func(int, string)) (string, int, error) {
		return "压缩摘要：旧对话已合并。", 100, nil
	}

	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.UserMessage("旧用户请求")); err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.AssistantMessage("旧助手回复", nil)); err != nil {
		t.Fatal(err)
	}
	conversation := NewSessionConversationForAgentWithRuntimeContexts(
		sess,
		&config.Config{},
		config.AgentKindIDE,
		"稳定作品上下文",
		"## 当前大纲\n\n主角进入废城。",
		"本轮动态作品状态",
		"## 当前进度\n\n刚抵达废城。",
	)
	history, err := conversation.PrepareMessages("继续写", "继续写")
	if err != nil {
		t.Fatal(err)
	}
	compacted, result, err := conversation.CompactContextIfNeeded(context.Background(), ContextCompactionInput{
		Messages: history,
		Force:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Triggered {
		t.Fatalf("expected compaction to trigger: %#v", result)
	}
	if len(compacted) < 2 {
		t.Fatalf("compacted messages too short: %#v", compacted)
	}
	if !isContextCompactionMessage(compacted[0]) {
		t.Fatalf("compaction summary should replace old history: %#v", messageContents(compacted))
	}
	currentTurn := compacted[len(compacted)-1].Content
	for _, want := range []string{"# 稳定作品上下文", "主角进入废城", "# 本轮动态作品状态", "刚抵达废城", "继续写"} {
		if !strings.Contains(currentTurn, want) {
			t.Fatalf("retained current turn missing %q after compaction: %s", want, currentTurn)
		}
	}
}

func TestSessionConversationCompactionSourceUsesSemanticToolProjection(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.UserMessage("读取第一章")); err != nil {
		t.Fatal(err)
	}
	readArgs := `{"file_path":"/workspace/chapters/ch00001.md","offset":1,"limit":2000}`
	if err := sess.AppendContextMessage(schema.AssistantMessage("", []schema.ToolCall{{
		ID: "call-read", Type: "function", Function: schema.FunctionCall{Name: "read_file", Arguments: readArgs},
	}})); err != nil {
		t.Fatal(err)
	}
	readResult := `{"schema":"workspace_file.read.v2","file_path":"/workspace/chapters/ch00001.md","offset":1,"limit":2000}` +
		"\n     1\t" + strings.Repeat("历史章节正文", 1000)
	if err := sess.AppendContextMessage(schema.ToolMessage(readResult, "call-read", schema.WithToolName("read_file"))); err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(schema.AssistantMessage("已读取并完成判断", nil)); err != nil {
		t.Fatal(err)
	}

	conversation := NewSessionConversation(
		sess,
		WithSessionContextConfig(&config.Config{}, config.AgentKindIDE),
	)
	source, _, _, _ := conversation.compactionIncrementalSource(true)
	if len(source) != 4 {
		t.Fatalf("compaction source = %#v", source)
	}
	if !strings.Contains(source[2].Content, retainedToolReceiptSchema) ||
		!strings.Contains(source[2].Content, `"path":"/workspace/chapters/ch00001.md"`) {
		t.Fatalf("compaction source should contain a re-readable file receipt: %s", source[2].Content)
	}
	if strings.Contains(source[2].Content, "历史章节正文") {
		t.Fatalf("compaction source must not resend historical file bodies: %s", source[2].Content)
	}
}

func TestSessionConversationCompactsOnlyMessagesAfterPreviousCompaction(t *testing.T) {
	previous := summarizeContextForCompaction
	defer func() { summarizeContextForCompaction = previous }()

	var capturedExisting string
	var capturedSource []*schema.Message
	summarizeContextForCompaction = func(_ context.Context, _ *config.Config, _ string, existingCheckpoint string, source []*schema.Message, _ string, _ int, _ contextCompactionPolicy, _ func(int, string)) (string, int, error) {
		capturedExisting = existingCheckpoint
		capturedSource = source
		return "新压缩摘要：旧目标与新增进展都已合并。", 200, nil
	}

	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	messages := []*schema.Message{
		schema.UserMessage("已压缩用户 1"),
		schema.AssistantMessage("已压缩助手 1", nil),
		schema.UserMessage("新增用户 2"),
		schema.AssistantMessage("新增助手 2", nil),
	}
	for _, msg := range messages {
		if err := sess.Append(msg); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sess.AppendContextCompaction(session.ContextCompaction{
		AgentKind:        config.AgentKindIDE,
		Epoch:            1,
		Summary:          "旧压缩摘要：用户 1 已处理。",
		SourceStartIndex: 0,
		SourceEndIndex:   2,
		RetainedTurns:    1,
	}); err != nil {
		t.Fatal(err)
	}

	conversation := NewSessionConversationForAgent(sess, &config.Config{}, config.AgentKindIDE)
	_, result, err := conversation.CompactContextIfNeeded(context.Background(), ContextCompactionInput{
		Messages:       sess.GetEffectiveMessages(),
		Force:          true,
		KeepLatestUser: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Triggered {
		t.Fatalf("expected compaction to trigger: %#v", result)
	}
	if capturedExisting != "旧压缩摘要：用户 1 已处理。" {
		t.Fatalf("existing memory = %q", capturedExisting)
	}
	got := messageContents(capturedSource)
	want := []string{"新增用户 2", "新增助手 2"}
	if len(got) != len(want) {
		t.Fatalf("source len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("source[%d] = %q, want %q; all=%#v", i, got[i], want[i], got)
		}
	}
	if record, ok := sess.LatestContextCompaction(config.AgentKindIDE); !ok || record.SourceStartIndex != 2 || record.SourceEndIndex != 4 {
		t.Fatalf("new compaction should record incremental source range, got ok=%v record=%#v", ok, record)
	}
}

func TestSessionConversationUsesCompactionSummaryRetainedTailAndAppendedMessages(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if err := sess.Append(schema.UserMessage("user " + string(rune('0'+i)))); err != nil {
			t.Fatal(err)
		}
		if err := sess.Append(schema.AssistantMessage("assistant "+string(rune('0'+i)), nil)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sess.AppendContextCompaction(session.ContextCompaction{
		AgentKind:        config.AgentKindIDE,
		Summary:          "用户目标：继续写作。",
		SourceStartIndex: 0,
		SourceEndIndex:   2,
		RetainedTurns:    2,
	}); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	conversation := NewSessionConversationForAgent(sess, cfg, config.AgentKindIDE)
	history, err := conversation.PrepareMessages("user 3", "agent user 3")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 6 {
		t.Fatalf("history length = %d, want 6: %#v", len(history), history)
	}
	if !isContextCompactionMessage(history[0]) || history[0].Role != schema.User {
		t.Fatalf("first message should be compaction summary: %#v", history[0])
	}
	if history[1].Content != "user 1" || history[2].Content != "assistant 1" || history[3].Content != "user 2" || history[4].Content != "assistant 2" || history[5].Content != "agent user 3" {
		t.Fatalf("unexpected compacted history tail: %#v", history)
	}
	if visible := sess.History(); len(visible) != 5 {
		t.Fatalf("visible raw history should include only raw messages and current user: %#v", visible)
	}
}

func TestSessionConversationKeepsPostCompactionTurnsUntilNextCompaction(t *testing.T) {
	store, err := session.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("default")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 5; i++ {
		if err := sess.Append(schema.UserMessage("user " + string(rune('0'+i)))); err != nil {
			t.Fatal(err)
		}
		if err := sess.Append(schema.AssistantMessage("assistant "+string(rune('0'+i)), nil)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sess.AppendContextCompaction(session.ContextCompaction{
		AgentKind:        config.AgentKindIDE,
		Summary:          "用户目标：继续写作。",
		SourceStartIndex: 0,
		SourceEndIndex:   4,
		RetainedTurns:    1,
	}); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	conversation := NewSessionConversationForAgent(sess, cfg, config.AgentKindIDE)
	history, err := conversation.PrepareMessages("user 6", "agent user 6")
	if err != nil {
		t.Fatal(err)
	}
	got := messageContents(history)
	want := []string{
		history[0].Content,
		"user 2",
		"assistant 2",
		"user 3",
		"assistant 3",
		"user 4",
		"assistant 4",
		"user 5",
		"assistant 5",
		"agent user 6",
	}
	if !isContextCompactionMessage(history[0]) {
		t.Fatalf("first message should be compaction summary: %#v", history[0])
	}
	if len(got) != len(want) {
		t.Fatalf("history length = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("history[%d] = %q, want %q; all=%#v", i, got[i], want[i], got)
		}
	}
}

func messageContents(messages []*schema.Message) []string {
	contents := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		contents = append(contents, msg.Content)
	}
	return contents
}

func TestContextLedgerPartsForConversationIncludesDomainFragments(t *testing.T) {
	log := newContextBuildLog(DefaultLoopPolicy().ContextLedger)
	log.add("用户输入", "本轮原始请求", "推门", "")
	conversation := &contextLedgerReportingConversation{parts: []ContextLedgerPart{{
		Source: "LoreContext", Title: "常驻资料", Bytes: 128, Chars: 64, Included: true,
	}}}

	parts := contextLedgerPartsForConversation(log, conversation, []*schema.Message{schema.UserMessage("推门")})
	if len(parts) != 2 || parts[0].Source != "用户输入" || parts[1].Source != "LoreContext" {
		t.Fatalf("domain context fragments were not merged into the durable ledger: %#v", parts)
	}
}

func TestContextLedgerPartsForConversationUsesPostCompactionMessages(t *testing.T) {
	log := newContextBuildLog(DefaultLoopPolicy().ContextLedger)
	log.add("文件引用", "removed.md", "压缩前引用正文", "")
	conversation := &finalContextLedgerReportingConversation{}
	finalMessages := []*schema.Message{
		NewContextCompactionSummaryMessage(2, "有界摘要"),
		schema.UserMessage("最终用户消息"),
	}

	parts := contextLedgerPartsForConversation(log, conversation, finalMessages)
	if len(parts) != 2 || parts[0].Source != "文件引用" || parts[0].Included || !parts[0].Truncated || !strings.Contains(parts[0].Note, "not_present_after_final_compaction") || parts[1].Source != "final_messages" || conversation.messageCount != len(finalMessages) || conversation.lastContent != "最终用户消息" {
		t.Fatalf("ledger reporter did not receive the post-compaction message list: parts=%#v conversation=%#v", parts, conversation)
	}
}

func TestSingleInstructionConversationReportsStablePrefixToContextLedger(t *testing.T) {
	conversation := &singleInstructionConversation{
		stableContextTitle:    "常驻资料（complete=true; revision=rev-1）",
		stableContext:         "世界规则正文",
		stableContextMaxBytes: 1024,
	}
	parts := conversation.ContextLedgerParts()
	if len(parts) != 1 || parts[0].Source != "ResidentLore" || parts[0].Limit != 1024 || parts[0].Hash == "" || !strings.Contains(parts[0].Note, "complete=true") || !strings.Contains(parts[0].Note, "message_max_bytes=1024") {
		t.Fatalf("stable resident prefix missing from durable ledger: %#v", parts)
	}
}

type contextLedgerReportingConversation struct {
	parts    []ContextLedgerPart
	metadata RunTraceMetadata
}

type finalContextLedgerReportingConversation struct {
	contextLedgerReportingConversation
	messageCount int
	lastContent  string
}

func (c *finalContextLedgerReportingConversation) ContextLedgerPartsForMessages(messages []*schema.Message) []ContextLedgerPart {
	c.messageCount = len(messages)
	if len(messages) > 0 && messages[len(messages)-1] != nil {
		c.lastContent = messages[len(messages)-1].Content
	}
	return []ContextLedgerPart{{Source: "final_messages", Included: true}}
}

func (c *contextLedgerReportingConversation) PrepareMessages(string, string) ([]*schema.Message, error) {
	return nil, nil
}
func (c *contextLedgerReportingConversation) AppendAssistant(string) error { return nil }
func (c *contextLedgerReportingConversation) MarkInterrupted(string, string, string) error {
	return nil
}
func (c *contextLedgerReportingConversation) PendingInterruption() *session.Interruption { return nil }
func (c *contextLedgerReportingConversation) ResolveInterruption(string) error           { return nil }
func (c *contextLedgerReportingConversation) ContextLedgerParts() []ContextLedgerPart {
	return append([]ContextLedgerPart(nil), c.parts...)
}
func (c *contextLedgerReportingConversation) RunTraceMetadata() RunTraceMetadata {
	return c.metadata
}
