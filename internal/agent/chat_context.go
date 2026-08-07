package agent

import (
	"errors"
	"fmt"
	"strings"

	"denova/internal/book"
	"denova/internal/prompts"
	"denova/internal/workspacepath"
)

const maxStyleRuleContextChars = 32000

func appendLoadedWritingSkill(message string, loaded LoadedWritingSkill, logs ...*contextBuildLog) string {
	name := strings.TrimSpace(loaded.Name)
	content := strings.TrimSpace(loaded.Content)
	if name == "" || content == "" {
		return message
	}
	var block strings.Builder
	block.WriteString("# 已加载的内置 Writing Skill\n\n")
	block.WriteString("本轮已直接加载内置 Writing Skill `")
	block.WriteString(name)
	block.WriteString("`；不要再调用 `skill` 工具加载同名 Skill。写作范围仍只由用户本轮自然语言指令决定。\n")
	if baseDirectory := strings.TrimSpace(loaded.BaseDirectory); baseDirectory != "" {
		block.WriteString("Skill 目录：")
		block.WriteString(baseDirectory)
		block.WriteString("\n")
	}
	block.WriteString("\n<writing_skill name=\"")
	block.WriteString(name)
	block.WriteString("\">\n")
	block.WriteString(content)
	block.WriteString("\n</writing_skill>")

	contentBlock := block.String()
	addContextLog(logs, "注入规则", "内置 Writing Skill（直接加载）", contentBlock,
		fmt.Sprintf("name=%s chars=%d", name, len([]rune(content))))
	return prependTurnAttachment(message, contentBlock)
}

// appendWritingSkillLoadHint 只提示动态 Writing Skill 名称；完整 SKILL.md
// 必须由模型在判断需要正文续写/创作时通过 skill 工具自行加载。
func appendWritingSkillLoadHint(message, skillName string, logs ...*contextBuildLog) string {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return message
	}
	var block strings.Builder
	block.WriteString("# Writing Skill 按需加载提示\n\n")
	block.WriteString("当前创作 Agent 选中的 Writing Skill 是 `")
	block.WriteString(skillName)
	block.WriteString("`。\n\n")
	block.WriteString("- 若本轮请求涉及小说正文续写、章节正文创作、正文重写或润色，且当前 Agent 已启用 `skill` 工具，请先调用 `skill` 工具加载 `")
	block.WriteString(skillName)
	block.WriteString("`，读取完整 SKILL.md 后再执行。\n")
	block.WriteString("- 若本轮请求是问答、分析、整理、大纲/设定讨论、配置、规划、征求建议，或仍在讨论是否写/怎么写，不要加载 Writing Skill，不要修改文件；先继续讨论并等待用户明确要求执行。即使消息提到“续写 / 改写 / 创作”等词，也不能把讨论对象误判成执行指令。\n")
	block.WriteString("- 在调用 `skill` 工具前，不要假装已经读取了该 Skill 的完整说明；写作范围仍只由用户本轮自然语言指令决定，不存在单独的 `writing_scope` 字段。")

	contentBlock := block.String()
	addContextLog(logs, "注入规则", "Writing Skill 按需加载", contentBlock, skillName)
	return prependTurnAttachment(message, contentBlock)
}

// appendReferenceContext 将用户引用的文件内容前置到本次 Agent 输入。
func appendReferenceContext(bookService *book.Service, message string, references []string, logs ...*contextBuildLog) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(prompts.ReferenceHeader))

	total := 0
	seen := make(map[string]bool)
	for _, ref := range references {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true

		sb.WriteString("\n## @")
		sb.WriteString(ref)
		sb.WriteString("\n")

		if total >= maxReferenceTotalBytes {
			sb.WriteString(prompts.ReferenceOverflowHint)
			addContextLog(logs, "文件引用", "@"+ref, prompts.ReferenceOverflowHint, "未读取：引用内容总量已超过限制")
			continue
		}

		content, n, err := readReferencedFile(bookService, ref, maxReferenceFileBytes, maxReferenceTotalBytes-total)
		total += n
		if err != nil {
			sb.WriteString("读取失败：")
			sb.WriteString(err.Error())
			sb.WriteString("\n")
			addContextLog(logs, "文件引用", "@"+ref, err.Error(), "读取失败")
			continue
		}
		addContextLog(logs, "文件引用", "@"+ref, content, "")

		sb.WriteString("```markdown\n")
		sb.WriteString(content)
		sb.WriteString("\n```\n")
	}

	return prependTurnAttachment(message, sb.String())
}

// appendLoreReferenceContext 将用户本轮明确引用的结构化资料条目前置到 Agent 输入。
func appendLoreReferenceContext(bookService *book.Service, message string, references []string, logs ...*contextBuildLog) string {
	var sb strings.Builder
	sb.WriteString("# 本轮明确引用的资料库条目\n\n以下资料来自结构化资料库，优先级高于泛化摘要；请在本轮创作或判断中优先遵守这些条目的已确认设定。\n")

	if bookService == nil || bookService.Workspace() == "" {
		sb.WriteString("\n资料库读取失败：当前 workspace 不可用。\n")
		addContextLog(logs, "资料库引用", "workspace", "当前 workspace 不可用", "读取失败")
		return prependTurnAttachment(message, sb.String())
	}

	items, err := book.NewLoreStore(bookService.Workspace()).List()
	if err != nil {
		sb.WriteString("\n资料库读取失败：")
		sb.WriteString(err.Error())
		sb.WriteString("\n")
		addContextLog(logs, "资料库引用", workspacepath.Rel(bookService.Workspace(), "lore", "items.json"), err.Error(), "读取失败")
		return prependTurnAttachment(message, sb.String())
	}

	byID := make(map[string]book.LoreItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	seen := make(map[string]bool)
	for _, ref := range references {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		item, ok := byID[ref]
		if !ok {
			sb.WriteString("\n## @资料:")
			sb.WriteString(ref)
			sb.WriteString("\n读取失败：资料条目不存在\n")
			addContextLog(logs, "资料库引用", "@资料:"+ref, "资料条目不存在", "读取失败")
			continue
		}
		content := formatLoreReference(item)
		addContextLog(logs, "资料库引用", "@资料:"+item.Name, content, item.ID)
		sb.WriteString("\n")
		sb.WriteString(content)
		sb.WriteString("\n")
	}

	return prependTurnAttachment(message, sb.String())
}

// styleRulesSystemInstruction 把工作区配置的「场景 → 文风参考」规则集作为 system prompt 片段。
func styleRulesSystemInstruction(rules []StyleRule) string {
	return prompts.StyleRulesInstruction(boundedStyleRules(rules, maxStyleRuleContextChars))
}

func boundedStyleRules(rules []StyleRule, maxChars int) []StyleRule {
	if maxChars <= 0 {
		return nil
	}
	result := make([]StyleRule, 0, len(rules))
	used := 0
	for _, rule := range rules {
		scene := strings.TrimSpace(rule.Scene)
		if !rule.Global && scene == "" {
			continue
		}
		if len(rule.StyleReferences) == 0 && len(rule.StyleContents) == 0 {
			continue
		}
		refs := make([]prompts.StyleReference, 0, len(rule.StyleReferences))
		for _, ref := range rule.StyleReferences {
			name := strings.TrimSpace(ref.Name)
			path := strings.TrimSpace(ref.Path)
			displayPath := strings.TrimSpace(ref.DisplayPath)
			if name == "" {
				name = displayPath
			}
			if path == "" {
				path = displayPath
			}
			if name == "" || path == "" {
				continue
			}
			desc := truncateRunes(strings.TrimSpace(ref.Description), 240)
			errText := truncateRunes(strings.TrimSpace(ref.Error), 240)
			remain := maxChars - used
			if remain <= 0 {
				break
			}
			cost := styleReferencePromptCost(name, desc, path, displayPath, errText)
			if cost > remain {
				used = maxChars
				break
			}
			used += cost
			refs = append(refs, prompts.StyleReference{
				Name:        name,
				Description: desc,
				Path:        path,
				DisplayPath: displayPath,
				Missing:     ref.Missing,
				Error:       errText,
			})
		}
		contents := make([]string, 0, len(rule.StyleContents))
		for _, content := range rule.StyleContents {
			content = strings.TrimSpace(content)
			if content == "" {
				continue
			}
			remain := maxChars - used
			if remain <= 0 {
				break
			}
			runes := []rune(content)
			if len(runes) > remain {
				content = string(runes[:remain]) + "\n\n[风格内容已截断]"
				used = maxChars
			} else {
				used += len(runes)
			}
			contents = append(contents, content)
		}
		if len(contents) > 0 || len(refs) > 0 {
			used += len([]rune(scene)) + 16
			result = append(result, StyleRule{Global: rule.Global, Scene: scene, StyleReferences: refs, StyleContents: contents})
		}
		if used >= maxChars {
			break
		}
	}
	return result
}

func styleReferencePromptCost(name, description, path, displayPath, errText string) int {
	return len([]rune(name)) + len([]rune(description)) + len([]rune(path)) + len([]rune(displayPath)) + len([]rune(errText)) + 64
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

// appendSelectionContext 将用户在编辑器中选中的文本片段前置到消息上下文。
func appendSelectionContext(message string, selections []TextSelectionRef) string {
	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(prompts.SelectionHeader))

	for _, sel := range selections {
		sb.WriteString("\n## 选中内容来自 ")
		sb.WriteString(sel.FileName)
		sb.WriteString(fmt.Sprintf(":L%d-L%d\n", sel.StartLine, sel.EndLine))
		sb.WriteString("```\n")
		sb.WriteString(sel.Content)
		sb.WriteString("\n```\n")
	}

	return prependTurnAttachment(message, sb.String())
}

// prependTurnAttachment keeps every turn-scoped rule, reference and selected
// span ahead of the raw request. Callers can add attachments incrementally
// without letting a large late attachment bury the authoritative instruction.
func prependTurnAttachment(message, attachment string) string {
	message = strings.TrimSpace(message)
	attachment = strings.TrimSpace(attachment)
	if attachment == "" {
		return message
	}
	if message == "" {
		return attachment
	}
	return attachment + "\n\n---\n\n" + message
}

// renderTurnInput preserves attachment order and makes the authoritative
// request the absolute tail of the final user message. This is deliberately a
// single render step: incrementally appending references or Skills after the
// request makes a short instruction easy to lose behind large context blocks.
func renderTurnInput(request string, attachments []string) string {
	var blocks []string
	for _, attachment := range attachments {
		if attachment = strings.TrimSpace(attachment); attachment != "" {
			blocks = append(blocks, attachment)
		}
	}
	var sb strings.Builder
	for i, block := range blocks {
		if i > 0 {
			sb.WriteString("\n\n---\n\n")
		}
		sb.WriteString(block)
	}
	if len(blocks) > 0 {
		sb.WriteString("\n\n---\n\n")
	}
	sb.WriteString("# 本轮用户请求（最高优先级）\n\n")
	sb.WriteString(strings.TrimSpace(request))
	return sb.String()
}

// readReferencedFile 安全读取引用文件，并按单文件和总大小限制截断。
func readReferencedFile(bookService *book.Service, relPath string, fileLimit, remainLimit int) (string, int, error) {
	limit := fileLimit
	if remainLimit < limit {
		limit = remainLimit
	}
	if limit <= 0 {
		return "", 0, errors.New("引用内容总量已超过限制")
	}

	content, err := bookService.ReadFile(relPath)
	if err != nil {
		return "", 0, err
	}

	data := []byte(content)
	truncated := false
	if len(data) > limit {
		data = data[:limit]
		truncated = true
	}

	result := string(data)
	if truncated {
		result += "\n\n[内容已截断]"
	}
	return result, len(data), nil
}

func formatLoreReference(item book.LoreItem) string {
	var sb strings.Builder
	sb.WriteString("## ")
	sb.WriteString(item.Name)
	sb.WriteString("（")
	sb.WriteString(item.Type)
	sb.WriteString(" / ")
	sb.WriteString(item.Importance)
	sb.WriteString(" / ")
	sb.WriteString(item.LoadMode)
	sb.WriteString("）\n")
	sb.WriteString("ID：")
	sb.WriteString(item.ID)
	sb.WriteString("\n")
	if len(item.Tags) > 0 {
		sb.WriteString("标签：")
		sb.WriteString(strings.Join(item.Tags, "、"))
		sb.WriteString("\n")
	}
	if item.BriefDescription != "" {
		sb.WriteString("简介：")
		sb.WriteString(item.BriefDescription)
		sb.WriteString("\n")
	}
	content := strings.TrimSpace(item.Content)
	if content != "" {
		sb.WriteString("\n```markdown\n")
		sb.WriteString(content)
		sb.WriteString("\n```\n")
	}
	return strings.TrimSpace(sb.String())
}
