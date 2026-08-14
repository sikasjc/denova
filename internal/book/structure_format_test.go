package book

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func newTestState(t *testing.T) *State {
	t.Helper()
	ws := t.TempDir()
	state := NewState(ws)
	if err := state.InitWorkspace(); err != nil {
		t.Fatalf("InitWorkspace failed: %v", err)
	}
	return state
}

// TestInitWorkspaceSeedsStructureFormatFiles verifies book init writes the two
// per-book structure template files with the default structure inside.
func TestInitWorkspaceSeedsStructureFormatFiles(t *testing.T) {
	state := newTestState(t)
	for _, name := range []string{OutlineFormatFileName, ChapterGroupFormatFileName} {
		path := filepath.Join(state.SettingDir(), name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to be seeded on init: %v", name, err)
		}
	}
	// The seeded outline-format file must carry the volume-level default structure.
	if got := state.OutlineFormatOverride(); !strings.Contains(got, "分卷规划") {
		t.Fatalf("seeded outline-format should contain the default volume-level structure, got: %q", got)
	}
	outline := state.OutlineFormatOverride()
	for _, want := range []string{"一句话简介", "核心剧情", "核心设定", "分卷规划", "本卷内容", "关键节点", "结束状态"} {
		if !strings.Contains(outline, want) {
			t.Fatalf("seeded outline-format should contain volume-level marker %q", want)
		}
	}
	for _, unwanted := range []string{"第1章", "第2章", "第X章", "本章目标", "## 主要人物"} {
		if strings.Contains(outline, unwanted) {
			t.Fatalf("seeded outline-format must not contain chapter/roster marker %q", unwanted)
		}
	}
}

func TestInitWorkspacePreservesExistingStructureFormatFiles(t *testing.T) {
	state := newTestState(t)
	path := filepath.Join(state.SettingDir(), OutlineFormatFileName)
	const custom = "# 作者已有的大纲结构"
	if err := os.WriteFile(path, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.InitWorkspace(); err != nil {
		t.Fatal(err)
	}
	if got := state.OutlineFormatOverride(); got != custom {
		t.Fatalf("InitWorkspace must not overwrite existing structure file: got %q want %q", got, custom)
	}
}

func TestStructureFormatOverrideEmptyWhenBlank(t *testing.T) {
	state := newTestState(t)
	if err := os.WriteFile(filepath.Join(state.SettingDir(), OutlineFormatFileName), []byte(" \n\t"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := state.OutlineFormatOverride(); got != "" {
		t.Fatalf("blank outline-format file should yield empty override, got %q", got)
	}
}

// TestStructureFormatOverrideBounded verifies an over-long file is capped so the
// injected system prompt stays bounded.
func TestStructureFormatOverrideBounded(t *testing.T) {
	state := newTestState(t)
	huge := strings.Repeat("段", MaxStructureFormatBytes)
	if err := os.WriteFile(filepath.Join(state.SettingDir(), OutlineFormatFileName), []byte(huge), 0o644); err != nil {
		t.Fatal(err)
	}
	got := state.OutlineFormatOverride()
	if len(got) > MaxStructureFormatBytes {
		t.Fatalf("override not capped: got %d bytes, cap %d", len(got), MaxStructureFormatBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatal("bounded structure format must remain valid UTF-8")
	}
}
