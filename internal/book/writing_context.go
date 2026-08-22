package book

import (
	"fmt"
	"sort"
	"strings"
)

const (
	// WritingStableContextMaxBytes reserves a compact, always-on baseline for
	// writing turns. Full lore bodies remain available through read_lore_items.
	WritingStableContextMaxBytes = 12 * 1024
	// WritingDynamicContextMaxBytes keeps the active writing plan and state
	// useful without allowing routine workspace growth to dominate every turn.
	WritingDynamicContextMaxBytes = 20 * 1024
	// WritingResidentLoreIndexMaxBytes bounds the names and metadata of resident
	// lore injected as a retrieval index, rather than injecting every body.
	WritingResidentLoreIndexMaxBytes = 4 * 1024
	writingIdeasContextMaxBytes      = 3 * 1024
	writingOutlineContextMaxBytes    = 5 * 1024
	writingChapterGroupMaxBytes      = 8 * 1024
	writingChapterPathsMaxBytes      = 2 * 1024
	writingCharacterStatesMaxBytes   = 10 * 1024
)

// WritingContextSnapshot is the bounded workspace context sent on every IDE
// writing turn. It intentionally contains an index for durable lore, not the
// resident lore bodies themselves; the Agent must load a body when needed.
type WritingContextSnapshot struct {
	StableParts  []CompactContextPart
	DynamicParts []CompactContextPart
}

// WritingContextSnapshot builds the writing-mode baseline from explicit,
// bounded sources. It is separate from CompactContext because that legacy
// helper represents the complete workspace snapshot and remains useful for
// administrative views and migrations.
func (s *State) WritingContextSnapshot() WritingContextSnapshot {
	if s == nil {
		return WritingContextSnapshot{}
	}
	stable := s.writingStableContextParts()
	dynamic := s.writingDynamicContextParts()
	return WritingContextSnapshot{
		StableParts:  fitWritingContextParts(stable, WritingStableContextMaxBytes),
		DynamicParts: fitWritingContextParts(dynamic, WritingDynamicContextMaxBytes),
	}
}

func (s *State) writingStableContextParts() []CompactContextPart {
	parts := append([]CompactContextPart(nil), s.StableContextParts()...)
	index := s.writingResidentLoreIndex()
	for i := range parts {
		if parts[i].ID != "lore" {
			continue
		}
		parts[i].Title = "常驻资料目录"
		parts[i].PromptTitle = "常驻资料目录（按需使用 read_lore_items 读取正文）"
		parts[i].Content = strings.TrimSpace(index)
		break
	}
	return parts
}

func (s *State) writingResidentLoreIndex() string {
	items, err := NewLoreStore(s.workspace).List()
	if err != nil {
		return ""
	}
	resident := make([]LoreItem, 0, len(items))
	for _, item := range items {
		if item.LoadMode == LoreLoadModeResident {
			resident = append(resident, item)
		}
	}
	if len(resident) == 0 {
		return ""
	}
	sort.SliceStable(resident, func(i, j int) bool { return resident[i].ID < resident[j].ID })
	var sb strings.Builder
	sb.WriteString("# 常驻资料目录\n\n")
	sb.WriteString("以下仅为可检索索引；需要设定正文时调用 read_lore_items。\n\n")
	for _, item := range resident {
		sb.WriteString(fmt.Sprintf("- id: %s\n  名称: %s\n  类型: %s\n  重要度: %s\n", item.ID, item.Name, item.Type, item.Importance))
		if sb.Len() >= WritingResidentLoreIndexMaxBytes {
			break
		}
	}
	return strings.TrimSpace(truncateStringBytes(sb.String(), WritingResidentLoreIndexMaxBytes))
}

func (s *State) writingDynamicContextParts() []CompactContextPart {
	parts := append([]CompactContextPart(nil), s.DynamicContextParts()...)
	for i := range parts {
		if parts[i].ID == "chapter_groups" {
			parts[i].Content = s.ChapterGroupContext(1)
			break
		}
	}
	return parts
}

func fitWritingContextParts(parts []CompactContextPart, maxBytes int) []CompactContextPart {
	if maxBytes <= 0 || len(parts) == 0 {
		return nil
	}
	result := make([]CompactContextPart, 0, len(parts))
	remaining := maxBytes
	for _, part := range parts {
		part.Content = strings.TrimSpace(part.Content)
		if part.Content == "" || remaining <= 0 {
			continue
		}
		limit := writingContextPartMaxBytes(part.ID)
		if limit <= 0 || limit > remaining {
			limit = remaining
		}
		if len(part.Content) > limit {
			part.Content = strings.TrimSpace(truncateStringBytes(part.Content, limit))
		}
		if part.Content == "" {
			continue
		}
		remaining -= len(part.Content)
		result = append(result, part)
	}
	return result
}

func writingContextPartMaxBytes(partID string) int {
	switch partID {
	case "ideas":
		return writingIdeasContextMaxBytes
	case "outline":
		return writingOutlineContextMaxBytes
	case "lore":
		return WritingResidentLoreIndexMaxBytes
	case "chapter_groups":
		return writingChapterGroupMaxBytes
	case "chapter_paths":
		return writingChapterPathsMaxBytes
	case "character_states":
		return writingCharacterStatesMaxBytes
	default:
		return 0
	}
}
