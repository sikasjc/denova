package skills

import (
	"context"
	"fmt"
	"sort"
	"strings"

	einoskill "github.com/cloudwego/eino/adk/middlewares/skill"
)

// Backend adapts multiple Nova skill directories to Eino's skill.Backend.
type Backend struct {
	dirs      []Directory
	agentKind string
	overrides map[string]bool
	delegates map[string]bool
}

type ResolvedSkill struct {
	Name          string
	Description   string
	Content       string
	BaseDirectory string
	Scope         Scope
}

func NewBackend(dirs []Directory) *Backend {
	return &Backend{dirs: dedupeDirectories(dirs)}
}

func NewAgentBackend(dirs []Directory, agentKind string, overrides map[string]bool) *Backend {
	return &Backend{dirs: dedupeDirectories(dirs), agentKind: strings.TrimSpace(agentKind), overrides: normalizeOverrideMap(overrides)}
}

func (b *Backend) WithDelegates(delegates map[string]bool) *Backend {
	if b == nil {
		return b
	}
	b.delegates = normalizeOverrideMap(delegates)
	return b
}

func (b *Backend) List(ctx context.Context) ([]einoskill.FrontMatter, error) {
	records := b.activeRecords(ctx)
	matters := make([]einoskill.FrontMatter, 0, len(records))
	for _, rec := range records {
		matters = append(matters, rec.skill.FrontMatter)
	}
	sort.Slice(matters, func(i, j int) bool {
		return matters[i].Name < matters[j].Name
	})
	return matters, nil
}

func (b *Backend) Get(ctx context.Context, name string) (einoskill.Skill, error) {
	for _, rec := range b.activeRecords(ctx) {
		if rec.skill.Name == name {
			if rec.delegate != "" && b.agentKind != rec.delegate {
				skill := rec.skill
				if b.delegates[rec.delegate] {
					skill.Content = delegatedSkillRouterContent(rec.skill.Name, rec.delegate)
				} else {
					skill.Content = delegatedSkillUnavailableContent(rec.skill.Name, rec.delegate)
				}
				return skill, nil
			}
			return rec.skill, nil
		}
	}
	return einoskill.Skill{}, fmt.Errorf("skill not found: %s", name)
}

func delegatedSkillRouterContent(skillName, delegate string) string {
	return fmt.Sprintf(`此 Skill 的完整方法只在专业 SubAgent %q 内执行。

必须调用 task 工具，并设置：
- subagent_type: %s
- description: 写清用户原始目标、必要上下文来源、文件路径或资源 ID、已知约束、期望只返回 beat sheet，且禁止写入。

不要在父 Agent 中自行执行或复述 %s 的完整编排方法。专业 SubAgent 返回节拍表后，父 Agent再按用户目标决定是直接展示，还是用于后续正文扩写。`, delegate, delegate, skillName)
}

func delegatedSkillUnavailableContent(skillName, delegate string) string {
	return fmt.Sprintf(`[%s BLOCKED]

专业 SubAgent %q 当前未启用或不属于本 Agent，不能安全执行此 Skill。
不要在父 Agent 中直接执行完整编排方法，也不要伪造 beat sheet。
请明确告诉用户：需要在 Agents 页为当前模式启用 %s 后重试。`, skillName, delegate, delegate)
}

func (b *Backend) Resolve(ctx context.Context, name string) (ResolvedSkill, error) {
	name = strings.TrimSpace(name)
	for _, rec := range b.activeRecords(ctx) {
		if rec.skill.Name != name {
			continue
		}
		return ResolvedSkill{
			Name:          rec.skill.Name,
			Description:   rec.skill.Description,
			Content:       rec.skill.Content,
			BaseDirectory: rec.skill.BaseDirectory,
			Scope:         rec.summary.Scope,
		}, nil
	}
	return ResolvedSkill{}, fmt.Errorf("skill not found: %s", name)
}
