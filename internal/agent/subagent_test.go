package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"denova/config"
	"denova/internal/session"
)

func TestConfigMaxIterationDefaultsToUnlimited(t *testing.T) {
	if got := configMaxIteration(&config.Config{}); got != unlimitedAgentMaxIterations {
		t.Fatalf("default max iteration = %d, want %d", got, unlimitedAgentMaxIterations)
	}
	if got := configMaxIteration(&config.Config{MaxIteration: 32}); got != 32 {
		t.Fatalf("configured max iteration = %d, want 32", got)
	}
}

func TestRequiredOutputAgentRejectsEmptyAssistantResult(t *testing.T) {
	inner := fakeEventAgent{
		name:        "reviewer",
		description: "review",
		events: []*adk.AgentEvent{{
			Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
				Message: schema.AssistantMessage("", nil),
			}},
		}},
	}
	agent := requiredOutputAgent{agent: inner}

	iterator := agent.Run(context.Background(), &adk.AgentInput{})
	var last *adk.AgentEvent
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		last = event
	}
	if last == nil || last.Err == nil || !strings.Contains(last.Err.Error(), "返回空结果") {
		t.Fatalf("empty reviewer result should become an error, got %#v", last)
	}
}

func TestBuildDeepAgentPassesGeneralAndConfiguredSubAgents(t *testing.T) {
	off := false
	var captured *deep.Config
	previous := newDeepAgent
	newDeepAgent = func(_ context.Context, cfg *deep.Config) (adk.ResumableAgent, error) {
		copied := *cfg
		captured = &copied
		return fakeAgent{name: cfg.Name, description: cfg.Description}, nil
	}
	t.Cleanup(func() { newDeepAgent = previous })

	_, err := buildDeepAgent(context.Background(), &config.Config{
		OpenAIBaseURL: "https://example.invalid",
		OpenAIModel:   "test-model",
		AgentTools: config.AgentToolSettings{
			Default: config.AgentToolOverride{
				FileRead:     &off,
				FileWrite:    &off,
				ShellExecute: &off,
				Skills:       &off,
				LoreRead:     &off,
				LoreWrite:    &off,
				Todo:         &off,
				WebSearch:    &off,
			},
		},
		SubAgents: []config.SubAgentConfig{{
			ID:           "researcher",
			Name:         "Researcher",
			Description:  "Researches delegated context",
			SystemPrompt: "Return concise findings.",
			Parents:      []string{config.AgentKindIDE},
		}},
	}, deepAgentSpec{
		Kind:        config.AgentKindIDE,
		Name:        "DenovaAgent",
		Description: "test",
		Instruction: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if captured == nil {
		t.Fatalf("expected deep config to be captured")
	}
	if captured.WithoutGeneralSubAgent {
		t.Fatalf("general subagent must stay enabled")
	}
	if !captured.ToolsConfig.EmitInternalEvents {
		t.Fatalf("parent DeepAgent should emit nested internal events")
	}
	if len(captured.SubAgents) != 1 {
		t.Fatalf("expected one configured subagent, got %d", len(captured.SubAgents))
	}
	if got := captured.SubAgents[0].Name(context.Background()); got != "researcher" {
		t.Fatalf("unexpected subagent name: %s", got)
	}
}

func TestBuildDeepAgentIncludesConfiguredSubAgents(t *testing.T) {
	off := false
	on := true
	var captured *deep.Config
	previous := newDeepAgent
	newDeepAgent = func(_ context.Context, cfg *deep.Config) (adk.ResumableAgent, error) {
		copied := *cfg
		captured = &copied
		return fakeAgent{name: cfg.Name, description: cfg.Description}, nil
	}
	t.Cleanup(func() { newDeepAgent = previous })

	configured := []config.SubAgentConfig{
		{ID: "reviewer", Description: "review", SystemPrompt: "review only", Enabled: &on, Parents: []string{config.AgentKindIDE}},
		{ID: "writer", Description: "write", SystemPrompt: "write draft", Enabled: &on, Parents: []string{config.AgentKindIDE}},
	}
	_, err := buildDeepAgent(context.Background(), &config.Config{
		OpenAIBaseURL: "https://example.invalid",
		OpenAIModel:   "test-model",
		AgentTools: config.AgentToolSettings{
			Default: config.AgentToolOverride{
				FileRead:     &off,
				FileWrite:    &off,
				ShellExecute: &off,
				Skills:       &off,
				LoreRead:     &off,
				LoreWrite:    &off,
				Todo:         &off,
				WebSearch:    &off,
			},
		},
		SubAgents: configured,
	}, deepAgentSpec{
		Kind:        config.AgentKindIDE,
		Name:        "DenovaAgent",
		Description: "test",
		Instruction: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if captured == nil || len(captured.SubAgents) != 2 {
		t.Fatalf("expected configured subagents wired into deep agent, got %#v", captured)
	}
	got := []string{
		captured.SubAgents[0].Name(context.Background()),
		captured.SubAgents[1].Name(context.Background()),
	}
	if strings.Join(got, ",") != "reviewer,writer" {
		t.Fatalf("unexpected wired subagents: %#v", got)
	}
	description, err := captured.TaskToolDescriptionGenerator(context.Background(), captured.SubAgents)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(description, "reviewer: review") || !strings.Contains(description, "writer: write") || len([]rune(description)) > 700 {
		t.Fatalf("task tool description must preserve available agents in a bounded form: %q", description)
	}
}

func TestAvailableSubAgentIDsRespectParentAndEnabledState(t *testing.T) {
	off := false
	available := availableSubAgentIDs(&config.Config{SubAgents: []config.SubAgentConfig{
		{
			ID:           "reviewer",
			Description:  "review",
			SystemPrompt: "route review",
			Parents:      []string{config.AgentKindIDE, config.AgentKindInteractiveStory},
		},
		{
			ID:           "writer",
			Description:  "write",
			SystemPrompt: "route write",
			Enabled:      &off,
			Parents:      []string{config.AgentKindIDE},
		},
	}}, config.AgentKindIDE)

	if !available["reviewer"] || available["writer"] {
		t.Fatalf("available delegates = %#v", available)
	}
}

func TestBuildDeepAgentCanDisableGeneralSubAgent(t *testing.T) {
	off := false
	var captured *deep.Config
	previous := newDeepAgent
	newDeepAgent = func(_ context.Context, cfg *deep.Config) (adk.ResumableAgent, error) {
		copied := *cfg
		captured = &copied
		return fakeAgent{name: cfg.Name, description: cfg.Description}, nil
	}
	t.Cleanup(func() { newDeepAgent = previous })

	_, err := buildDeepAgent(context.Background(), &config.Config{
		OpenAIBaseURL: "https://example.invalid",
		OpenAIModel:   "test-model",
		GeneralSubAgents: config.AgentGeneralSubAgentSettings{
			IDE: &off,
		},
		AgentTools: config.AgentToolSettings{
			Default: config.AgentToolOverride{
				FileRead:     &off,
				FileWrite:    &off,
				ShellExecute: &off,
				Skills:       &off,
				LoreRead:     &off,
				LoreWrite:    &off,
				Todo:         &off,
				WebSearch:    &off,
			},
		},
	}, deepAgentSpec{
		Kind:        config.AgentKindIDE,
		Name:        "DenovaAgent",
		Description: "test",
		Instruction: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if captured == nil || !captured.WithoutGeneralSubAgent {
		t.Fatalf("general subagent should be disabled when configured off: %#v", captured)
	}
}

func TestSubAgentAssemblyUsesParentToolPolicyKind(t *testing.T) {
	assembly, err := buildChatModelAgentAssembly(context.Background(), &config.Config{}, chatModelAgentAssemblySpec{
		Kind:           "researcher",
		ToolPolicyKind: config.AgentKindInteractiveStory,
		ToolSettings: config.ResolvedAgentToolSettings{
			FileRead:     false,
			FileWrite:    false,
			ShellExecute: false,
			Skills:       false,
			LoreRead:     false,
			LoreWrite:    false,
			Todo:         false,
			WebSearch:    false,
		},
		IncludeCompaction: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	var orchestrator *toolOrchestratorMiddleware
	for _, handler := range assembly.Handlers {
		if middleware, ok := handler.(*toolOrchestratorMiddleware); ok {
			orchestrator = middleware
			break
		}
	}
	if orchestrator == nil {
		t.Fatalf("expected tool orchestrator middleware")
	}
	if got := orchestrator.effectivePolicyKind(); got != config.AgentKindInteractiveStory {
		t.Fatalf("subagent tool policy should use parent kind, got %q", got)
	}
}

func TestBuildChatModelAgentAssemblyPassesToolResultLimit(t *testing.T) {
	assembly, err := buildChatModelAgentAssembly(context.Background(), &config.Config{AgentToolResultLimitKB: 64}, chatModelAgentAssemblySpec{
		Kind: config.AgentKindIDE,
		ToolSettings: config.ResolvedAgentToolSettings{
			FileRead:     false,
			FileWrite:    false,
			ShellExecute: false,
			Skills:       false,
			LoreRead:     false,
			LoreWrite:    false,
			Todo:         false,
			WebSearch:    false,
		},
		IncludeCompaction: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	var orchestrator *toolOrchestratorMiddleware
	for _, handler := range assembly.Handlers {
		if middleware, ok := handler.(*toolOrchestratorMiddleware); ok {
			orchestrator = middleware
			break
		}
	}
	if orchestrator == nil {
		t.Fatalf("expected tool orchestrator middleware")
	}
	if got := orchestrator.toolResultLimitBytes(); got != 64*1024 {
		t.Fatalf("tool result limit bytes = %d, want %d", got, 64*1024)
	}
}

func TestWritingAssemblyPublishesReplacementToolsInsteadOfEditFile(t *testing.T) {
	assembly, err := buildChatModelAgentAssembly(context.Background(), &config.Config{Workspace: t.TempDir()}, chatModelAgentAssemblySpec{
		Kind: config.AgentKindIDE,
		ToolSettings: config.ResolvedAgentToolSettings{
			FileRead:  true,
			FileWrite: true,
		},
		IncludeCompaction: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	runCtx := &adk.ChatModelAgentContext{Tools: append([]tool.BaseTool(nil), assembly.Tools...)}
	for _, handler := range assembly.Handlers {
		_, runCtx, err = handler.BeforeAgent(context.Background(), runCtx)
		if err != nil {
			t.Fatal(err)
		}
	}
	names := make(map[string]bool, len(runCtx.Tools))
	for _, base := range runCtx.Tools {
		info, err := base.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		names[info.Name] = true
	}
	for _, expected := range []string{"replace_lines", "replace_text", "write_file"} {
		if !names[expected] {
			t.Fatalf("missing workspace write tool %q: %v", expected, names)
		}
	}
	if names["edit_file"] {
		t.Fatalf("legacy edit_file must not remain model-visible: %v", names)
	}
	for _, absent := range []string{"glob", "execute"} {
		if names[absent] {
			t.Fatalf("default writing assembly must not expose %q: %v", absent, names)
		}
	}
}

func TestSubAgentStreamingDoesNotAppendParentAssistantContent(t *testing.T) {
	var fullContent, fullThinking strings.Builder
	var events []Event
	meta := agentEventMetadata{AgentName: "researcher", RootAgentName: "DenovaAgent", RunPath: []string{"DenovaAgent", "researcher"}, SubAgent: true}
	processNonStreamingEvent(&adk.MessageVariant{Message: schema.AssistantMessage("sub draft", nil)}, &fullContent, &fullThinking, 0, meta, nil, func(ev Event) {
		events = append(events, ev)
	})
	if fullContent.Len() != 0 || fullThinking.Len() != 0 {
		t.Fatalf("subagent output must not append to parent builders content=%q thinking=%q", fullContent.String(), fullThinking.String())
	}
	if len(events) != 1 || events[0].Type != "chunk" || !eventDataBool(events[0].Data, "subagent") {
		t.Fatalf("subagent chunk should still be emitted with metadata: %#v", events)
	}

	rootMeta := agentEventMetadata{AgentName: "DenovaAgent", RootAgentName: "DenovaAgent", RunPath: []string{"DenovaAgent"}}
	processNonStreamingEvent(&adk.MessageVariant{Message: schema.AssistantMessage("root final", nil)}, &fullContent, &fullThinking, 0, rootMeta, nil, func(Event) {})
	if got := fullContent.String(); got != "root final" {
		t.Fatalf("root output should append to parent builder, got %q", got)
	}
}

func TestDisplayRecorderPersistsSubAgentAssistantChunks(t *testing.T) {
	appender := &fakeDisplayAppender{}
	recorder := newDisplayEventRecorder(fakeDisplayConversation{appender: appender})
	meta := agentEventMetadata{
		RunID:             "run-1",
		AgentName:         "researcher",
		RootAgentName:     "DenovaAgent",
		ModelProfileID:    "flash",
		ModelName:         "deepseek-v4-flash",
		RunPath:           []string{"DenovaAgent", "researcher"},
		SubAgent:          true,
		SubAgentSessionID: "run-1-subagent-01-researcher",
		SubAgentType:      "researcher",
	}

	recorder.Record(Event{Type: "chunk", Data: meta.appendTo(map[string]interface{}{"content": "第一段"})})
	recorder.Record(Event{Type: "chunk", Data: meta.appendTo(map[string]interface{}{"content": "第二段"})})

	if len(appender.events) != 1 {
		t.Fatalf("expected one merged display event, got %#v", appender.events)
	}
	event := appender.events[0]
	if event.Role != "assistant" || event.Content != "第一段第二段" {
		t.Fatalf("unexpected persisted subagent event: %#v", event)
	}
	if !event.SubAgent || event.SubAgentSessionID != "run-1-subagent-01-researcher" || event.SubAgentType != "researcher" {
		t.Fatalf("subagent metadata missing: %#v", event)
	}
	if event.ModelProfileID != "flash" || event.ModelName != "deepseek-v4-flash" {
		t.Fatalf("subagent model metadata missing: %#v", event)
	}
}

func TestSubAgentWriteToolResultStillTracksMutation(t *testing.T) {
	tracker := newMutationTracker()
	tracker.Observe(Event{Type: "tool_call", Data: map[string]interface{}{
		"id":       "call-write",
		"name":     "write_file",
		"args":     `{"file_path":"chapters/ch01.md","content":"new"}`,
		"subagent": true,
	}})
	tracker.Observe(Event{Type: "tool_result", Data: map[string]interface{}{
		"id":       "call-write",
		"name":     "write_file",
		"content":  "ok",
		"subagent": true,
	}})
	mutations := tracker.Mutations()
	if len(mutations) != 1 {
		t.Fatalf("expected subagent write tool to be tracked, got %#v", mutations)
	}
	if mutations[0].Target != "chapters/ch01.md" || !mutations[0].RequiresPostCheck {
		t.Fatalf("unexpected mutation: %#v", mutations[0])
	}
}

type fakeDisplayConversation struct {
	appender *fakeDisplayAppender
}

func (c fakeDisplayConversation) PrepareMessages(_, _ string) ([]*schema.Message, error) {
	return nil, nil
}
func (c fakeDisplayConversation) AppendAssistant(string) error               { return nil }
func (c fakeDisplayConversation) MarkInterrupted(_, _, _ string) error       { return nil }
func (c fakeDisplayConversation) PendingInterruption() *session.Interruption { return nil }
func (c fakeDisplayConversation) ResolveInterruption(string) error           { return nil }
func (c fakeDisplayConversation) AppendDisplayEvent(event session.DisplayEvent) error {
	return c.appender.AppendDisplayEvent(event)
}
func (c fakeDisplayConversation) UpdateDisplayToolStatus(id, name, status string) error {
	return c.appender.UpdateDisplayToolStatus(id, name, status)
}
func (c fakeDisplayConversation) AppendDisplayEventContent(id, role, delta string) error {
	return c.appender.AppendDisplayEventContent(id, role, delta)
}

type fakeDisplayAppender struct {
	events []session.DisplayEvent
}

func (a *fakeDisplayAppender) AppendDisplayEvent(event session.DisplayEvent) error {
	a.events = append(a.events, event)
	return nil
}

func (a *fakeDisplayAppender) UpdateDisplayToolStatus(_, _, _ string) error { return nil }

func (a *fakeDisplayAppender) AppendDisplayEventContent(id, role, delta string) error {
	for index := range a.events {
		if a.events[index].ID == id && a.events[index].Role == role {
			a.events[index].Content += delta
			return nil
		}
	}
	return nil
}

type fakeAgent struct {
	name        string
	description string
}

type fakeEventAgent struct {
	name        string
	description string
	events      []*adk.AgentEvent
}

func (f fakeEventAgent) Name(context.Context) string        { return f.name }
func (f fakeEventAgent) Description(context.Context) string { return f.description }
func (f fakeEventAgent) Run(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		for _, event := range f.events {
			generator.Send(event)
		}
		generator.Close()
	}()
	return iterator
}

func (f fakeAgent) Name(context.Context) string        { return f.name }
func (f fakeAgent) Description(context.Context) string { return f.description }
func (f fakeAgent) Run(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Close()
	return iter
}
func (f fakeAgent) Resume(context.Context, *adk.ResumeInfo, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iter, gen := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	gen.Close()
	return iter
}
