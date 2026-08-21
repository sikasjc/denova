package agent

import (
	"log"
	"strings"

	"github.com/cloudwego/eino/schema"

	"denova/config"
)

// ToolResultContextPolicy controls whether completed tool exchanges can cross
// user-turn boundaries. Result size is bounded once, when a tool returns; this
// policy only keeps valid pairs and applies domain-specific semantic filtering.
type ToolResultContextPolicy struct {
	AgentKind      string
	Enabled        bool
	MaxResultBytes int
	// RetainedProseMaxBytes bounds the total bytes of read_file bodies kept
	// across turns, either verbatim (file unchanged) or refreshed to the
	// current content (file changed). 0 disables prose retention (read_file
	// always collapses to a receipt at assembly time).
	RetainedProseMaxBytes int
}

// ToolResultRevisionResolver returns the current full-file revision for an
// absolute workspace path. ok is false when the path cannot be resolved to a
// bounded revision (missing, oversized, or outside the workspace). A nil
// resolver disables verbatim prose retention entirely — every retained
// read_file body collapses to a receipt — preserving the pre-revision behavior
// for the compaction source and non-IDE assembly paths.
type ToolResultRevisionResolver func(path string) (string, bool)

// ToolResultFileResolver bundles the lookups used to keep read_file bodies
// usable across turns. Revision is the cheap stat-backed freshness check;
// Window, when non-nil, re-reads the current content of one offset/limit
// window so a body whose file changed can be REFRESHED in place instead of
// collapsing to a "please re-read" receipt (the model's own edits are the most
// common reason a body goes stale). A nil Window preserves the collapse-only
// behavior.
type ToolResultFileResolver struct {
	Revision ToolResultRevisionResolver
	Window   ToolResultWindowResolver
}

func resolveToolResultContextPolicy(cfg *config.Config, agentKind string) ToolResultContextPolicy {
	settings := config.ResolveAgentContext(cfg, agentKind)
	return ToolResultContextPolicy{
		AgentKind:             strings.TrimSpace(agentKind),
		Enabled:               settings.ToolResultRetentionEnabled,
		MaxResultBytes:        configToolResultMaxBytes(cfg),
		RetainedProseMaxBytes: settings.RetainedProseMaxBytes,
	}
}

func ResolveToolResultContextPolicyForConversation(cfg *config.Config, agentKind string) ToolResultContextPolicy {
	return resolveToolResultContextPolicy(cfg, agentKind)
}

func (p ToolResultContextPolicy) normalized() ToolResultContextPolicy {
	p.MaxResultBytes = normalizeToolResultLimitBytes(p.MaxResultBytes)
	return p
}

type toolResultContextConversation interface {
	AppendContextMessage(msg *schema.Message) error
	ToolResultContextPolicy() ToolResultContextPolicy
}

type toolResultContextRecorder struct {
	conversation    toolResultContextConversation
	policy          ToolResultContextPolicy
	retainedCallIDs map[string]struct{}
}

func newToolResultContextRecorder(conversation Conversation) *toolResultContextRecorder {
	contextConversation, ok := conversation.(toolResultContextConversation)
	if !ok || contextConversation == nil {
		return &toolResultContextRecorder{}
	}
	policy := contextConversation.ToolResultContextPolicy().normalized()
	if !policy.Enabled {
		return &toolResultContextRecorder{}
	}
	return &toolResultContextRecorder{conversation: contextConversation, policy: policy}
}

func (r *toolResultContextRecorder) RecordAssistantToolCalls(msg *schema.Message, meta agentEventMetadata) {
	if r == nil || r.conversation == nil || meta.SubAgent || msg == nil || len(msg.ToolCalls) == 0 {
		return
	}
	next := assistantToolContextMessage(msg, r.policy)
	if next == nil {
		return
	}
	if err := r.conversation.AppendContextMessage(next); err != nil {
		logAgentContextPersistError("assistant_tool_calls", err)
		return
	}
	if r.retainedCallIDs == nil {
		r.retainedCallIDs = make(map[string]struct{}, len(next.ToolCalls))
	}
	for _, call := range next.ToolCalls {
		if callID := strings.TrimSpace(call.ID); callID != "" {
			r.retainedCallIDs[callID] = struct{}{}
		}
	}
}

func (r *toolResultContextRecorder) RecordToolResult(toolName, toolCallID, content string, meta agentEventMetadata) {
	if r == nil || r.conversation == nil || meta.SubAgent || isPlanProtocolToolName(toolName) || !retainToolContextAcrossTurns(toolName, r.policy) || !r.retainedCall(toolCallID) {
		return
	}
	msg := schema.ToolMessage(recordTimeToolResultContent(toolName, content, r.policy), toolCallID, schema.WithToolName(toolName))
	if err := r.conversation.AppendContextMessage(msg); err != nil {
		logAgentContextPersistError("tool_result", err)
	}
}

func (r *toolResultContextRecorder) retainedCall(toolCallID string) bool {
	if r == nil {
		return false
	}
	_, ok := r.retainedCallIDs[strings.TrimSpace(toolCallID)]
	return ok
}

func logAgentContextPersistError(kind string, err error) {
	log.Printf("[agent-run] persist tool result context failed kind=%s err=%v", kind, err)
}

func assistantToolContextMessage(msg *schema.Message, policy ToolResultContextPolicy) *schema.Message {
	if msg == nil || len(msg.ToolCalls) == 0 {
		return nil
	}
	calls := make([]schema.ToolCall, 0, len(msg.ToolCalls))
	for _, call := range msg.ToolCalls {
		if isPlanProtocolToolName(call.Function.Name) || !retainToolContextAcrossTurns(call.Function.Name, policy) {
			continue
		}
		next := call
		arguments, valid := retainedToolCallArguments(next.Function.Arguments)
		if !valid {
			continue
		}
		next.Function.Arguments = arguments
		calls = append(calls, next)
	}
	if len(calls) == 0 {
		return nil
	}
	return schema.AssistantMessage("", calls)
}

func retainedToolCallArguments(arguments string) (string, bool) {
	if err := validateToolArgumentsJSON(arguments); err != nil {
		return "", false
	}
	if strings.TrimSpace(arguments) == "" {
		return "{}", true
	}
	return arguments, true
}

func toolResultContextContent(toolName, content string, policy ToolResultContextPolicy) string {
	return semanticToolResultContextContent(toolName, content, policy)
}

// recordTimeToolResultContent decides what a retained tool result stores when it
// is first recorded. read_file bodies are stored verbatim so a later assembly can
// keep them when the file is unchanged (cache-stable, no re-read) and only fall
// back to a receipt when the revision moved or the byte budget is exceeded. All
// other retained tools (read_lore_items, skill) have no revision concept, so they
// collapse to their receipt immediately, exactly as before.
func recordTimeToolResultContent(toolName, content string, policy ToolResultContextPolicy) string {
	if normalizeToolName(toolName) == "read_file" {
		return content
	}
	return semanticToolResultContextContent(toolName, content, policy)
}

func applyToolResultContextPolicy(messages []*schema.Message, policy ToolResultContextPolicy) []*schema.Message {
	return applyToolResultContextPolicyWithResolver(messages, policy, ToolResultFileResolver{})
}

// applyToolResultContextPolicyWithResolver applies the retention policy and,
// when a revision resolver is supplied, keeps read_file bodies usable across
// turns up to the policy budget: unchanged bodies stay verbatim (cache-stable
// prefix) and changed bodies are refreshed to the file's current window when a
// window resolver is available. An empty resolver reproduces the pre-revision
// behavior: every retained read_file body collapses to a receipt.
func applyToolResultContextPolicyWithResolver(messages []*schema.Message, policy ToolResultContextPolicy, resolver ToolResultFileResolver) []*schema.Message {
	if len(messages) == 0 {
		return messages
	}
	policy = policy.normalized()
	if !policy.Enabled {
		return removeToolContextMessages(messages)
	}
	return filterSemanticToolContextMessages(messages, policy, resolver)
}

func sanitizedToolContextMessage(msg *schema.Message, policy ToolResultContextPolicy) *schema.Message {
	if msg == nil || msg.Role != schema.Tool {
		return msg
	}
	content := semanticToolResultContextContent(msg.ToolName, msg.Content, policy)
	if content == msg.Content {
		return msg
	}
	next := *msg
	next.Content = content
	return &next
}

func ApplyToolResultContextPolicyForConversation(messages []*schema.Message, policy ToolResultContextPolicy) []*schema.Message {
	return applyToolResultContextPolicy(messages, policy)
}

func removeToolContextMessages(messages []*schema.Message) []*schema.Message {
	filtered := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		if msg.Role == schema.Tool {
			continue
		}
		if msg.Role == schema.Assistant && len(msg.ToolCalls) > 0 {
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			next := *msg
			next.ToolCalls = nil
			filtered = append(filtered, &next)
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}
