package config

import (
	"strconv"
	"strings"
)

const (
	MaxWritingQuickActions           = 24
	MaxWritingQuickActionIDRunes     = 80
	MaxWritingQuickActionLabelRunes  = 80
	MaxWritingQuickActionPromptRunes = 262144
)

type WritingIntent string

const (
	WritingIntentAuto              WritingIntent = ""
	WritingIntentPlanning          WritingIntent = "planning"
	WritingIntentProseGeneration   WritingIntent = "prose_generation"
	WritingIntentProseRevision     WritingIntent = "prose_revision"
	WritingIntentReviewApplication WritingIntent = "review_application"
	WritingIntentAnalysis          WritingIntent = "analysis"
)

// WritingQuickAction describes one user-configurable Writing Agent draft shortcut.
// Prompt is inserted into the composer as a user message and remains editable before send.
type WritingQuickAction struct {
	ID     string        `toml:"id" json:"id"`
	Label  string        `toml:"label,omitempty" json:"label,omitempty"`
	Prompt string        `toml:"prompt" json:"prompt"`
	Intent WritingIntent `toml:"intent,omitempty" json:"intent,omitempty"`
}

func sanitizeWritingQuickActions(actions *[]WritingQuickAction) *[]WritingQuickAction {
	if actions == nil {
		return nil
	}
	source := *actions
	if len(source) > MaxWritingQuickActions {
		source = source[:MaxWritingQuickActions]
	}
	sanitized := make([]WritingQuickAction, 0, len(source))
	usedIDs := make(map[string]struct{}, len(source))
	for index, action := range source {
		id := truncateWritingQuickActionRunes(strings.TrimSpace(action.ID), MaxWritingQuickActionIDRunes)
		if id == "" {
			id = "custom-" + strconv.Itoa(index+1)
		}
		baseID := id
		for suffix := 2; ; suffix++ {
			if _, exists := usedIDs[id]; !exists {
				break
			}
			id = baseID + "-" + strconv.Itoa(suffix)
		}
		usedIDs[id] = struct{}{}
		sanitized = append(sanitized, WritingQuickAction{
			ID:     id,
			Label:  truncateWritingQuickActionRunes(strings.TrimSpace(action.Label), MaxWritingQuickActionLabelRunes),
			Prompt: truncateWritingQuickActionRunes(strings.TrimSpace(action.Prompt), MaxWritingQuickActionPromptRunes),
			Intent: NormalizeWritingIntent(action.Intent),
		})
	}
	return &sanitized
}

func NormalizeWritingIntent(intent WritingIntent) WritingIntent {
	switch WritingIntent(strings.TrimSpace(string(intent))) {
	case WritingIntentPlanning:
		return WritingIntentPlanning
	case WritingIntentProseGeneration:
		return WritingIntentProseGeneration
	case WritingIntentProseRevision:
		return WritingIntentProseRevision
	case WritingIntentReviewApplication:
		return WritingIntentReviewApplication
	case WritingIntentAnalysis:
		return WritingIntentAnalysis
	default:
		return WritingIntentAuto
	}
}

func truncateWritingQuickActionRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}
