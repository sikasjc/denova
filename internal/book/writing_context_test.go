package book

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritingContextSnapshotBoundsWorkspaceAndIndexesResidentLore(t *testing.T) {
	dir := t.TempDir()
	state := NewState(dir)
	if err := state.InitWorkspace(); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(state.SettingDir(), "outline.md"), []byte(strings.Repeat("大纲内容。", 4_000)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state.SettingDir(), CharacterStatesFileName), []byte(strings.Repeat("角色状态。", 7_000)), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		path := filepath.Join(state.ChapterGroupDir(), fmt.Sprintf("group%02d.md", i))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("章节组 %d。", i)+strings.Repeat("细纲。", 6_000)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewLoreStore(dir).Create(LoreItemInput{
		ID:         "hero",
		Type:       "character",
		Name:       "主角",
		Importance: "major",
		LoadMode:   LoreLoadModeResident,
		Content:    strings.Repeat("只应按需读取的长期设定。", 2_000),
	}); err != nil {
		t.Fatalf("create lore: %v", err)
	}

	snapshot := state.WritingContextSnapshot()
	stable := FormatCompactContextParts(snapshot.StableParts)
	dynamic := FormatCompactContextParts(snapshot.DynamicParts)
	if len(stable) > WritingStableContextMaxBytes+1024 {
		t.Fatalf("stable context exceeds bounded budget: %d", len(stable))
	}
	if len(dynamic) > WritingDynamicContextMaxBytes+1024 {
		t.Fatalf("dynamic context exceeds bounded budget: %d", len(dynamic))
	}
	if !strings.Contains(stable, "id: hero") || strings.Contains(stable, "只应按需读取的长期设定") {
		t.Fatalf("stable context must expose a lore index without resident bodies: %s", stable)
	}
	if !strings.Contains(dynamic, "group02.md") || strings.Contains(dynamic, "group01.md") {
		t.Fatalf("dynamic context must retain only the latest chapter group: %s", dynamic)
	}
	if !strings.Contains(dynamic, "角色状态") {
		t.Fatalf("dynamic context must retain a bounded character-state source: %s", dynamic)
	}
}
