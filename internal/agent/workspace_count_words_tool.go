package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"denova/internal/book"
)

const workspaceCountWordsResultSchema = "workspace_count_words.v1"

// workspaceCountWordsToolDescription 明确要求模型汇报字数时以本工具结果为准：
// LLM 自行数中文字符极易出错，必须用工具数字纠正 review 输出。
var workspaceCountWordsToolDescription = strings.TrimSpace(`Count writing word statistics with the exact same rule as the Denova UI: one non-whitespace character counts as one word (CJK text counts per character).
- Call without paths to get whole-book statistics: every chapter's words in reading order, the chapter total, and word counts of outline/detailed-outline documents.
- Call with paths to count specific workspace files (absolute or workspace-relative); each missing file is reported in its own entry instead of failing the whole call.
- start_line/end_line (1-based, inclusive, replace_lines line numbers) optionally restrict counting to one line range applied to every given path; omit both to count whole files. Use them to measure a section inside a chapter instead of re-counting by hand.
- When reporting or reviewing chapter lengths, quote these numbers verbatim; never estimate word counts by reading the text.

使用与 Denova 界面完全一致的字数口径统计写作字数：一个非空白字符记 1 字（中文按字数统计）。
- 不传 paths 时返回全书统计：按阅读顺序排列的每章字数、章节总字数，以及大纲/细纲等文档的字数。
- 传入 paths 时统计指定的 workspace 文件（绝对路径或相对路径均可）；缺失文件在对应条目内报告错误，不会导致整个调用失败。
- start_line/end_line（1-based、含端点、与 replace_lines 行号一致）可选地把统计限定在同一线性范围内，作用于每个传入路径；两者都省略时统计整个文件。需要统计章节内某一段时使用，避免手动估算。
- 汇报或审阅章节字数时必须直接引用这些数字，禁止通过阅读正文自行估算字数。`)

type workspaceCountWordsInput struct {
	Paths     []string `json:"paths,omitempty" jsonschema:"description=Optional specific workspace file paths (absolute or workspace-relative); omit to return whole-book chapter statistics"`
	StartLine int      `json:"start_line,omitempty" jsonschema:"description=Optional 1-based first line (inclusive, replace_lines numbering) applied to every path; requires paths and must be used with end_line semantics"`
	EndLine   int      `json:"end_line,omitempty" jsonschema:"description=Optional inclusive 1-based last line applied to every path; 0 means through end of file"`
}

type workspaceCountWordsChapter struct {
	Path   string `json:"path"`
	Title  string `json:"title"`
	Volume string `json:"volume,omitempty"`
	Words  int    `json:"words"`
}

type workspaceCountWordsDocument struct {
	Path  string `json:"path"`
	Title string `json:"title"`
	Words int    `json:"words"`
}

type workspaceCountWordsFileCount struct {
	Path      string `json:"path"`
	Words     int    `json:"words"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Error     string `json:"error,omitempty"`
}

type workspaceCountWordsResult struct {
	Schema       string                         `json:"schema"`
	Mode         string                         `json:"mode"`
	TotalWords   int                            `json:"total_words"`
	ChapterCount int                            `json:"chapter_count,omitempty"`
	Chapters     []workspaceCountWordsChapter   `json:"chapters,omitempty"`
	Documents    []workspaceCountWordsDocument  `json:"documents,omitempty"`
	Files        []workspaceCountWordsFileCount `json:"files,omitempty"`
}

// newWorkspaceCountWordsTool 提供 count_words 工具：全书模式复用 book.Summary，
// 与界面章节统计同源；指定路径模式用 book.CountWritingWords 保证同一口径。
func newWorkspaceCountWordsTool(workspace string) (tool.BaseTool, error) {
	return utils.InferTool("count_words", workspaceCountWordsToolDescription, func(_ context.Context, input workspaceCountWordsInput) (string, error) {
		root := strings.TrimSpace(workspace)
		if root == "" {
			return "", fmt.Errorf("当前 workspace 不可用，无法统计字数")
		}
		service := book.NewService(root)
		if len(input.Paths) > 0 {
			return countWorkspaceFilesWords(service, root, input.Paths, input.StartLine, input.EndLine)
		}
		if input.StartLine != 0 || input.EndLine != 0 {
			return "", fmt.Errorf("start_line/end_line 只能与 paths 一起使用 / start_line/end_line require paths")
		}
		return countWorkspaceBookWords(service)
	})
}

func countWorkspaceBookWords(service *book.Service) (string, error) {
	summary, err := service.Summary()
	if err != nil {
		return "", err
	}
	result := workspaceCountWordsResult{
		Schema:       workspaceCountWordsResultSchema,
		Mode:         "book",
		TotalWords:   summary.TotalWords,
		ChapterCount: summary.ChapterCount,
	}
	for _, chapter := range summary.Chapters {
		result.Chapters = append(result.Chapters, workspaceCountWordsChapter{
			Path:   chapter.Path,
			Title:  chapter.DisplayTitle,
			Volume: chapter.Volume,
			Words:  chapter.Words,
		})
	}
	for _, document := range summaryChapterDocuments(summary) {
		result.Documents = append(result.Documents, document)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("serialize count_words result: %w", err)
	}
	return string(data), nil
}

// summaryChapterDocuments 把 Summary 里的大纲/灵感/细纲预览压平成文档字数
// 条目；total_words 只包含正文章节，与界面 TotalWords 一致。
func summaryChapterDocuments(summary book.WorkspaceSummary) []workspaceCountWordsDocument {
	documents := make([]workspaceCountWordsDocument, 0, 2+len(summary.ChapterPlans))
	for _, preview := range []*book.DocumentPreview{summary.Ideas, summary.Outline} {
		if preview != nil {
			documents = append(documents, workspaceCountWordsDocument{Path: preview.Path, Title: preview.Title, Words: preview.Words})
		}
	}
	for _, plan := range summary.ChapterPlans {
		documents = append(documents, workspaceCountWordsDocument{Path: plan.Path, Title: plan.Title, Words: plan.Words})
	}
	return documents
}

func countWorkspaceFilesWords(service *book.Service, workspace string, paths []string, startLine, endLine int) (string, error) {
	startLine, endLine, err := normalizeCountWordsLineRange(startLine, endLine)
	if err != nil {
		return "", err
	}
	result := workspaceCountWordsResult{
		Schema: workspaceCountWordsResultSchema,
		Mode:   "files",
	}
	for _, raw := range paths {
		rel, relErr := workspaceCountWordsRelPath(workspace, raw)
		if relErr != nil {
			result.Files = append(result.Files, workspaceCountWordsFileCount{Path: strings.TrimSpace(raw), Error: relErr.Error()})
			continue
		}
		content, readErr := service.ReadFile(rel)
		if readErr != nil {
			result.Files = append(result.Files, workspaceCountWordsFileCount{Path: rel, Error: readErr.Error()})
			continue
		}
		words, effectiveEnd, countErr := countWritingWordsInLines(content, startLine, endLine)
		if countErr != nil {
			result.Files = append(result.Files, workspaceCountWordsFileCount{Path: rel, Error: countErr.Error()})
			continue
		}
		entry := workspaceCountWordsFileCount{Path: rel, Words: words}
		if startLine > 0 {
			// 回显实际生效范围：end_line 超出末行时截断到末行，方便模型核对。
			entry.StartLine = startLine
			entry.EndLine = effectiveEnd
		}
		result.Files = append(result.Files, entry)
		result.TotalWords += words
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("serialize count_words result: %w", err)
	}
	return string(data), nil
}

// normalizeCountWordsLineRange 归一化可选的行区间：0 表示未设置；只给其中
// 一端时另一端取全文边界；end_line 早于 start_line 视为用法错误。
func normalizeCountWordsLineRange(startLine, endLine int) (int, int, error) {
	if startLine < 0 || endLine < 0 {
		return 0, 0, fmt.Errorf("start_line/end_line must be 1-based line numbers / start_line/end_line 必须是 1-based 行号")
	}
	if startLine == 0 && endLine == 0 {
		return 0, 0, nil
	}
	if startLine == 0 {
		startLine = 1
	}
	if endLine == 0 {
		endLine = math.MaxInt
	}
	if endLine < startLine {
		return 0, 0, fmt.Errorf("end_line %d is before start_line %d / end_line 不能早于 start_line", endLine, startLine)
	}
	return startLine, endLine, nil
}

// countWritingWordsInLines 统计 1-based 闭区间 [startLine, endLine] 内的字数。
// 行语义与 read_file 完全一致：按 \n 分行并去掉末尾换行产生的幻影空行，
// 这样模型从 read_file 看到的行号可以直接复用；startLine=0 表示统计整个文件。
func countWritingWordsInLines(content string, startLine, endLine int) (words, effectiveEnd int, err error) {
	if startLine == 0 {
		return book.CountWritingWords(content), 0, nil
	}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if startLine > len(lines) {
		return 0, 0, fmt.Errorf("start_line %d is beyond the last line %d / start_line 超出文件末行 %d", startLine, len(lines), len(lines))
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	return book.CountWritingWords(strings.Join(lines[startLine-1:endLine], "\n")), endLine, nil
}

// workspaceCountWordsRelPath 把模型给的绝对/相对路径规范成 workspace 内的
// slash 相对路径；越界路径返回错误而不是静默丢弃。
func workspaceCountWordsRelPath(workspace, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(input) {
		_, rel, err := resolveWorkspaceReadPath(workspace, input)
		if err != nil {
			return "", err
		}
		return rel, nil
	}
	rel := filepath.ToSlash(filepath.Clean(input))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("path is outside the active workspace: %s", input)
	}
	return rel, nil
}
