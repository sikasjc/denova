package book

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"denova/internal/prompts"
)

// Per-book structure template files. They live under setting/ alongside outline.md
// so they are discoverable in the book-settings surface and editable by the author
// or the Agent (write_file). When a file is absent or blank, the writing Agent falls
// back to the built-in structure rendered by the prompts package.
const (
	// OutlineFormatFileName 定义本书大纲结构的文件名（存于 setting/）。
	OutlineFormatFileName = "outline-format.md"
	// ChapterGroupFormatFileName 定义本书细纲结构的文件名（存于 setting/）。
	ChapterGroupFormatFileName = "chapter-group-format.md"
)

// MaxStructureFormatBytes bounds a per-book outline/chapter-group structure file.
// The template is injected into the writing system prompt every turn, so it must
// have an explicit source and hard cap. Keep this comfortably above 128 KiB so
// detailed user-authored templates are not truncated prematurely.
const MaxStructureFormatBytes = 256 * 1024

// OutlineFormatOverride 返回本书自定义的大纲结构（setting/outline-format.md）。
// 文件缺失或为空时返回空串，调用方据此回落系统内置默认结构。结果受 MaxStructureFormatBytes 上限约束。
func (s *State) OutlineFormatOverride() string {
	return boundedStructureFormat(s.readSettingFile(OutlineFormatFileName))
}

// ChapterGroupFormatOverride 返回本书自定义的细纲结构（setting/chapter-group-format.md）。
// 文件缺失或为空时返回空串，调用方据此回落系统内置默认结构。结果受 MaxStructureFormatBytes 上限约束。
func (s *State) ChapterGroupFormatOverride() string {
	return boundedStructureFormat(s.readSettingFile(ChapterGroupFormatFileName))
}

// boundedStructureFormat trims and caps a structure template read from disk so the
// injected system prompt stays bounded.
func boundedStructureFormat(content string) string {
	content = strings.TrimSpace(content)
	if len(content) <= MaxStructureFormatBytes {
		return content
	}
	return strings.TrimSpace(truncateStringBytes(content, MaxStructureFormatBytes))
}

// ensureStructureFormatFiles 在缺失时写入本书的大纲/细纲结构模板文件（仅当文件不存在）。
// 与 ensureCreatorTemplate 一致：不覆盖作者已有内容。
func (s *State) ensureStructureFormatFiles() error {
	files := []struct {
		name     string
		template string
	}{
		{OutlineFormatFileName, prompts.OutlineFormatFileTemplate},
		{ChapterGroupFormatFileName, prompts.ChapterGroupFormatFileTemplate},
	}
	for _, f := range files {
		path := filepath.Join(s.SettingDir(), f.name)
		if _, err := os.Stat(path); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("检查 %s 失败: %w", f.name, err)
		}
		if err := os.WriteFile(path, []byte(f.template), 0o644); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", f.name, err)
		}
	}
	return nil
}
