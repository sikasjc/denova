package prompts

import (
	"strings"
	"testing"
)

func TestSystemInstructionRequiresIdeasAndCreatorDuringIdeation(t *testing.T) {
	instruction := BuildSystemInstruction(SystemInstructionInput{
		Workspace: "/tmp/book",
	})

	for _, required := range []string{
		"/tmp/book/CREATOR.md",
		"/tmp/book/ideas.md",
		"新书构思阶段也必须基于模板和作者确认更新",
		"先 read_file ideas.md 和 CREATOR.md",
		"阶段性结论和待确认点",
		"CREATOR.md 负责“这本书长期怎么写、哪些规则必须一直遵守”",
		"每章字数/篇幅目标",
		"及时 edit_file 或 write_file 更新 ideas.md",
		"先分别 write_file 更新 ideas.md 和 CREATOR.md",
		"ideas.md 继续作为方向指引",
		"CREATOR.md 继续作为每轮最高优先级创作者指令生效",
		"内容保持短小、可扫读、方便作者评论和后续更新",
		"建议控制在 800-1200 个中文字内",
		"每章安排只写 3-5 条关键点",
		"ch{order:05}-{chapter}-{title}.md",
		"v{order:05}-{volume}",
		"不要自动重命名旧章节",
		"最小必要改动集",
		"审阅意见数量增加不代表允许扩大修改范围",
		"只有用户明确要求“整章重写 / 全文重写 / 换视角重写 / 彻底改写”",
		"old_string 应从读取结果中逐字复制",
		"不得因为精确匹配失败就改用 write_file 覆盖已有章节",
		"严格按当前生效 Writing Skill 规定的审稿、修订和最终机械验证顺序形成最终稿",
		"本规则不新增 Skill 之外的独立自审或修订阶段",
		"不要在 Skill 的 reviewer、fixer 或 final-gate 之前额外插入父 Agent 自审",
	} {
		if !strings.Contains(instruction, required) {
			t.Fatalf("系统提示缺少 %q:\n%s", required, instruction)
		}
	}
	for _, forbidden := range []string{
		"完成正文自检和本轮最后修订",
		"使用 grep 工具去检索并确认",
	} {
		if strings.Contains(instruction, forbidden) {
			t.Fatalf("系统提示不应以通用自审规则覆盖 Writing Skill 阶段 %q:\n%s", forbidden, instruction)
		}
	}
	if strings.Contains(instruction, "# 当前作品状态") {
		t.Fatalf("系统提示不应直接注入动态作品状态:\n%s", instruction)
	}
}

func TestIDEWritingFlowKeepsChapterStatusIndependentFromStateSync(t *testing.T) {
	instruction := BuildIDEWritingFlowInstruction(SystemInstructionInput{
		Workspace: "/tmp/book",
	})

	for _, required := range []string{
		"章节创作 -> 同步进度与角色状态",
		"章节正文直接写入 chapters/",
		"非空未确认章节可在 UI 中显示为初稿",
		"章节状态只是编辑标记",
		"不影响下一章判断、上下文选择或状态同步",
		"write_file 到 chapters/",
		"在同一轮更新 setting/progress.md 和 setting/character-states.md",
		"不等待作者另行确认成章",
	} {
		if !strings.Contains(instruction, required) {
			t.Fatalf("写作流程提示缺少 %q:\n%s", required, instruction)
		}
	}
	for _, forbidden := range []string{
		"草稿" + "流程",
		"draft" + "s/",
		"Draft" + "Flow",
		"章节草稿应先写入",
		"普通初稿不写入全书事实状态",
		"只有作者明确确认成章",
	} {
		if strings.Contains(instruction, forbidden) {
			t.Fatalf("写作流程提示不应包含旧草稿目录流程 %q:\n%s", forbidden, instruction)
		}
	}
	if strings.Contains(instruction, "%!(EXTRA") {
		t.Fatalf("写作流程提示存在多余 fmt 参数:\n%s", instruction)
	}
}

func TestCreatorTemplateDefersHardRuleGrepToFinalMechanicalVerification(t *testing.T) {
	for _, required := range []string{
		"按当前 Writing Skill 的阶段顺序",
		"在最终机械验证阶段使用 grep 检索确认",
		"不要因此在 reviewer、fixer 或 final-gate 前额外增加父 Agent 自审",
	} {
		if !strings.Contains(CreatorTemplate, required) {
			t.Fatalf("CreatorTemplate 缺少审稿阶段边界 %q:\n%s", required, CreatorTemplate)
		}
	}
	if strings.Contains(CreatorTemplate, "使用 grep 工具去检索并确认") {
		t.Fatalf("CreatorTemplate 不应再要求通用的预审 grep:\n%s", CreatorTemplate)
	}
}
