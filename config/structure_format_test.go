package config

import (
	"strings"
	"testing"
)

// TestStructureFormatConfigToSettings verifies the outline/chapter-group format
// fields survive the Config -> Settings mapping used to re-emit user config.
func TestStructureFormatConfigToSettings(t *testing.T) {
	outline := "# 自定义大纲\n\n## 主线"
	group := "# groupXX\n\n## 目标"
	cfg := &Config{OutlineFormat: outline, ChapterGroupFormat: group}
	back := settingsFromConfig(cfg)
	if back.OutlineFormat != outline || back.ChapterGroupFormat != group {
		t.Fatalf("Config->Settings lost structure formats: outline=%q group=%q", back.OutlineFormat, back.ChapterGroupFormat)
	}
}

// TestStructureFormatMergeOverride verifies a later layer replaces the earlier
// template only when non-empty (empty means "inherit / use built-in default").
func TestStructureFormatMergeOverride(t *testing.T) {
	parent := Settings{OutlineFormat: "parent-outline", ChapterGroupFormat: "parent-group"}
	child := Settings{OutlineFormat: "child-outline"} // ChapterGroupFormat empty: keep parent
	merged := Merge(parent, child)
	if merged.OutlineFormat != "child-outline" {
		t.Fatalf("non-empty child outline should win: %q", merged.OutlineFormat)
	}
	if merged.ChapterGroupFormat != "parent-group" {
		t.Fatalf("empty child chapter-group should keep parent: %q", merged.ChapterGroupFormat)
	}
}

// TestStructureFormatSanitizeCap verifies an over-long template is truncated to
// the bounded cap so the injected system prompt stays bounded.
func TestStructureFormatSanitizeCap(t *testing.T) {
	huge := strings.Repeat("段", MaxStructureFormatRunes+500)
	sanitized := sanitizeEditableSettings(Settings{OutlineFormat: huge})
	if runes := []rune(sanitized.OutlineFormat); len(runes) > MaxStructureFormatRunes {
		t.Fatalf("outline format not capped: got %d runes, cap %d", len(runes), MaxStructureFormatRunes)
	}
}
