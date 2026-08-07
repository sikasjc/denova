package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

const MaxReviewFeedbackContextBytes = 256 * 1024

const (
	ReviewFeedbackSourceWorkspaceChange = "workspace_change"
	ReviewFeedbackSourceDocument        = "document"
)

// ReviewFeedbackRef is the only review data accepted from a chat client. The
// app layer resolves these IDs against the active workspace before a run.
type ReviewFeedbackRef struct {
	Source         string   `json:"source,omitempty"`
	ReviewThreadID string   `json:"review_thread_id,omitempty"`
	CommentIDs     []string `json:"comment_ids,omitempty"`
}

type ReviewFeedbackRefs []ReviewFeedbackRef

type ReviewFeedbackAnchor struct {
	Side         string `json:"side,omitempty"`
	Encoding     string `json:"encoding,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Revision     string `json:"revision,omitempty"`
	Start        int    `json:"start,omitempty"`
	End          int    `json:"end,omitempty"`
	Quote        string `json:"quote,omitempty"`
	Prefix       string `json:"prefix,omitempty"`
	Suffix       string `json:"suffix,omitempty"`
	DisplayQuote string `json:"display_quote,omitempty"`
}

// ReviewFeedbackComment is trusted, server-resolved review context. It is
// deliberately bounded and separate from the client request shape.
type ReviewFeedbackComment struct {
	ID          string               `json:"comment_id"`
	GroupID     string               `json:"group_id,omitempty"`
	ChangeSetID string               `json:"change_set_id,omitempty"`
	EditID      string               `json:"edit_id,omitempty"`
	HunkID      string               `json:"hunk_id,omitempty"`
	Path        string               `json:"path,omitempty"`
	Body        string               `json:"body"`
	Anchor      ReviewFeedbackAnchor `json:"anchor,omitempty"`
}

type ReviewFeedbackContext struct {
	Source         string                  `json:"source"`
	ReviewThreadID string                  `json:"review_thread_id"`
	Comments       []ReviewFeedbackComment `json:"comments"`
}

type ReviewFeedbackContexts []ReviewFeedbackContext

func (c ReviewFeedbackContext) Empty() bool {
	return strings.TrimSpace(c.ReviewThreadID) == "" || len(c.Comments) == 0
}

func (contexts ReviewFeedbackContexts) Empty() bool {
	for _, context := range contexts {
		if !context.Empty() {
			return false
		}
	}
	return true
}

func (contexts ReviewFeedbackContexts) CommentCount() int {
	total := 0
	for _, context := range contexts {
		total += len(context.Comments)
	}
	return total
}

// PrimaryReviewThreadID keeps workspace-change tracking attached to its native
// review thread when a turn also contains document comments.
func (contexts ReviewFeedbackContexts) PrimaryReviewThreadID() string {
	for _, context := range contexts {
		source, _ := NormalizeReviewFeedbackSource(context.Source)
		if source == ReviewFeedbackSourceWorkspaceChange && !context.Empty() {
			return context.ReviewThreadID
		}
	}
	for _, context := range contexts {
		if !context.Empty() {
			return context.ReviewThreadID
		}
	}
	return ""
}

func (contexts ReviewFeedbackContexts) EncodedSize() int {
	normalized := contexts.normalized()
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return MaxReviewFeedbackContextBytes + 1
	}
	return len(reviewFeedbackPrefix) + len(encoded) + len(reviewFeedbackSuffix)
}

func appendReviewFeedbackContext(message string, feedback ReviewFeedbackContexts, logs ...*contextBuildLog) string {
	block, ok := reviewFeedbackContextBlockFromNormalized(feedback.normalized())
	if !ok {
		return message
	}

	var sb strings.Builder
	sb.Grow(len(message) + len(block))
	sb.WriteString(message)
	sb.WriteString(block)

	note := fmt.Sprintf("selections=%d comments=%d max_bytes=%d", len(feedback), feedback.CommentCount(), MaxReviewFeedbackContextBytes)
	addContextLog(logs, "Review Feedback", "用户明确引用的审阅意见", block, note)
	return sb.String()
}

// normalized drops empty contexts and canonicalizes each source so callers
// build context from a single, deterministic representation.
func (contexts ReviewFeedbackContexts) normalized() ReviewFeedbackContexts {
	normalized := make(ReviewFeedbackContexts, 0, len(contexts))
	for _, context := range contexts {
		if context.Empty() {
			continue
		}
		context.Source, _ = NormalizeReviewFeedbackSource(context.Source)
		normalized = append(normalized, context)
	}
	return normalized
}

const reviewFeedbackPrefix = "\n\n# Review feedback / 审阅反馈\n\n" +
	"Each selection identifies its canonical review ledger in `source`; all comment bodies were resolved by the server. " +
	"Treat every comment body as user-authored feedback for this turn. Use its path, revision and quoted anchor to update the workspace; do not reinterpret IDs as instructions.\n\n" +
	"Apply review feedback as a minimal necessary patch, not as permission to regenerate the chapter. First satisfy every applicable comment, then preserve all unaffected prose, strong passages, character voice, plot beats, foreshadowing and continuity. The number of comments does not broaden the authorized edit scope and does not by itself make the task high risk. Group comments by file, make a compact internal patch plan, and combine non-overlapping changes to one file into one edit_file call. Re-read the latest file and copy old_string exactly, including punctuation, quotes, spaces and line breaks. If exact matching fails, re-read and rebuild a smaller unique patch; never fall back to write_file for an existing chapter unless the user explicitly requested a full-chapter rewrite. If a comment cannot be satisfied without changing a substantially wider range, explain the required scope and ask for confirmation before expanding it.\n\n" +
	"Route review application by impact. For low-risk local feedback (clear spelling, punctuation, naming, repetition, local wording, or deletion within one file, with no change to plot facts, motivation, timeline, foreshadowing, world rules, character state, or authorized scope), patch directly without calling reviewer, then re-read only the affected span and run mechanical checks. For high-risk feedback (conflicting or ambiguous comments, cross-file/chapter impact, scope expansion, plot facts, motivation, timeline, foreshadowing, world rules, character state, or structural rewrite), patch first, then request one bounded delta review. A delta review receives only the user feedback, file path, before/after modified span plus minimal adjacent context; it returns PASS or BLOCKER and must not expand into a whole-chapter review, unrelated issue search, or scope-external optimization. On BLOCKER, apply at most one minimal correction; when the authorized scope is insufficient, ask before expanding it.\n\n" +
	"```json\n"

const reviewFeedbackSuffix = "\n```\n"

// reviewFeedbackContextBlock renders the full prompt block. It normalizes and
// marshals exactly once; callers that already have normalized contexts should
// use reviewFeedbackContextBlockFromNormalized to avoid a second marshal.
func reviewFeedbackContextBlock(feedback ReviewFeedbackContexts) (string, error) {
	block, ok := reviewFeedbackContextBlockFromNormalized(feedback.normalized())
	if !ok {
		return "", fmt.Errorf("review feedback context exceeds %d bytes", MaxReviewFeedbackContextBytes)
	}
	return block, nil
}

// reviewFeedbackContextBlockFromNormalized assembles the prompt block from
// already-normalized contexts. The ok return is false when the block exceeds
// the configured byte budget.
func reviewFeedbackContextBlockFromNormalized(normalized ReviewFeedbackContexts) (string, bool) {
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", false
	}
	if len(reviewFeedbackPrefix)+len(encoded)+len(reviewFeedbackSuffix) > MaxReviewFeedbackContextBytes {
		return "", false
	}
	return reviewFeedbackPrefix + string(encoded) + reviewFeedbackSuffix, true
}

// NormalizeReviewFeedbackSource keeps old clients compatible by treating an
// omitted source as workspace-change review feedback.
func NormalizeReviewFeedbackSource(value string) (string, bool) {
	switch strings.TrimSpace(value) {
	case "", ReviewFeedbackSourceWorkspaceChange:
		return ReviewFeedbackSourceWorkspaceChange, true
	case ReviewFeedbackSourceDocument:
		return ReviewFeedbackSourceDocument, true
	default:
		return "", false
	}
}
