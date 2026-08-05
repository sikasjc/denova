---
name: novel-heavy
description: 关键内容、复杂剧情和长篇连续性要求高的写作流程；先规划、综合审稿、再生成状态更新。
agent: ide
---

# novel-heavy

用于关键场景、多章剧情、结构性修改和长链路连续性任务。质量优先，允许完整多 Agent 流程。

## 适用范围

- 多章或剧情 arc、关键高潮/揭示、多角色复杂场景。
- 跨章节连续性、时间线、伏笔和复杂人物关系。
- 审阅意见互相冲突或涉及结构性调整。

## 执行

- 默认流程：`context-planner -> writer -> reviewer -> fixer -> final-gate -> memory-patcher -> final output`。
- 若 Context Plan 识别出下述“复杂编排”条件，则流程变为：`context-planner -> choreographer/intimacy-choreographer -> writer -> reviewer -> fixer -> final-gate -> memory-patcher -> final output`。
- 从用户实际指令判断范围；没有 `writing_scope` 字段。除非用户明确说“写下一章”，否则不要假设任务一定是下一章。
- 当用户要求一次写 N 章或多段 arc 时，Context Plan 必须包含整体计划和分章计划。
- 所有角色 subagent 都必须通过 `task` 工具委派。每次调用 `task` 时，在 description 中写清角色名、用户目标、必要上下文来源、文件路径、允许/禁止写入、期望输出格式和交付物。
- `context-planner`、`reviewer`、`final-gate`、`memory-patcher` 默认只返回计划、审稿、检查或 patch，不直接改文件；`writer` 和 `fixer` 是否写文件由主 Agent 的委派说明决定。主 Agent 对最终落盘结果负责。
- reviewer 只负责发现和说明问题；主 Agent 在交给 fixer 前必须聚合 reviewer 与用户审阅意见，生成最小必要 Patch Plan，写清每项问题、证据位置、必须保留内容、最小修改范围和重叠/冲突关系。
- fixer 完整解决 blocker/major 和确需处理的 minor，但遵循 system prompt 的最小必要 Patch；已有章节默认使用 `edit_file`。
- 工具返回 `[tool error]`、`string not found`、参数或路径错误时不得宣称已完成；重新读取并重试。
- Final Gate 后读回最终章节关键片段；memory-patcher 只基于最终稿更新确实变化的状态。

如果这些角色 subagent 可用，请按顺序使用：

1. 使用 `task` 工具委派 `context-planner` 整理 Context Plan。
2. 仅当 Context Plan 标记复杂编排风险时，使用 `task` 委派一个对应 choreography SubAgent 生成 beat sheet。
3. 使用 `task` 工具委派 `writer`，同时提供 Context Plan 和可选 beat sheet，根据计划生成正文。
4. 使用 `task` 工具委派 `reviewer` 做一次综合审稿。
5. 主 Agent 聚合 reviewer 与用户审阅意见，生成最小必要 Patch Plan，再使用 `task` 工具委派 `fixer` 定点修复。
6. 使用 `task` 工具委派 `final-gate` 检查修订稿是否满足用户要求、计划、canon 和风格约束。
7. 使用 `task` 工具委派 `memory-patcher` 生成 progress 和 character-state 等状态更新。
8. 主 Agent 输出最终结果，以及必要的用户可见状态更新摘要。

## 复杂编排

Context Plan 必须判断是否需要一个专业 choreography SubAgent：

- 复杂战斗、追逐、多人协作、救援/攀爬、军阵、载具或灾害，需要跨多拍跟踪位置、资源、环境与反应因果时，使用 `choreographer`。
- 复杂亲密/情色互动，需要跨多拍跟踪多人或长段肢体位置、姿态、情绪回应、意愿信号与张力时，使用 `intimacy-choreographer`。
- 普通对白、静态描写、简单走位、单次明确动作、单次拥抱/亲吻/触碰或纯文风修改，不需要 choreography。

每个独立场景最多调用一个对应 SubAgent 一次；多章任务按场景判断，不按章节机械调用。description 传递用户目标、必要路径、人物/空间/canon 约束、只返回 beat sheet、禁止写入；亲密场景原样传递用户尺度，不自行升级或净化。beat sheet 只作为 writer 内部约束，除非用户要求，不直接展示。

## Context Plan

写作前先生成轻量计划，格式如下：

```md
# Context Plan

## Writing Scope
本次要写什么范围，例如一段、一个场景、一章、N 章、一个剧情 arc。

## Goal
本次写作要完成的剧情目标。

## Required Beats
必须发生的关键事件。

## Character State
主要角色当前状态、动机、关系、已知信息。

## Canon Constraints
世界观、时间线、地点、道具、能力、伏笔等不能违背的约束。

## Style Constraints
叙事人称、文风、节奏、禁用表达。

## Risks
本次最容易写崩的地方。

## Choreography Need
是否需要 `choreographer` 或 `intimacy-choreographer`；若需要，写清场景、风险、路径和用户尺度/风格；否则写 `none` 和理由。
```

如果用户要求一次写 N 章，补充：

- `整体计划`: 共享剧情弧线、升级节奏、转折点和结束状态。
- `分章计划`: 每章一段简洁计划，包含章节目标、关键事件、POV 或焦点、结尾钩子或状态。

## 审稿协议

reviewer 必须返回结构化问题，每项包含：

- `severity`: `blocker` / `major` / `minor`
- `dimension`: `continuity` / `character_voice` / `pacing` / `prose` / `dialogue` / `plot_logic` / `style` / `user_requirement`
- `problem`
- `fix_instruction`
- `keep`

## Final Gate

- 只有修订稿满足用户要求、Context Plan、canon 约束、风格约束和明显连续性检查时才通过。
- 如果存在 blocker，把稿件带着明确指令交回 fixer 一次。
- 不要增加额外 reviewer agent。

## Memory Patch

最终稿完成后，`memory-patcher` 必须生成这些更新：

- `progress`: 剧情、时间线、地点、风险、未解决线索的变化。
- `character_state`: 当前状态、动机、关系变化、伤病、已知信息、资源、承诺和秘密。
- `world_state`: 只记录本轮即时故事状态中已经变化的事实。
- `foreshadowing`: 新埋、推进、兑现或退场的伏笔。

主 Agent 应在工具权限允许时把 `progress` 和 `character_state` 更新写入工作区对应状态文件；如果当前上下文无法确认文件路径，或用户明确要求只输出正文，则输出可应用的 patch 并说明未写入原因。

完整章节写入或实质性剧情改写完成后，状态更新必须在同一轮基于最终正文落盘，不等待作者另行确认成章；章节状态只用于 UI 编辑标记。纯错字、标点或措辞润色没有改变叙事事实时无需生成状态更新。

写入状态更新时必须使用文件工具：局部更新用 `edit_file`，全量重写用 `write_file`；写入后用 `read_file` 验证关键条目已经存在。

长期稳定资料库不同于 progress 和 character-state：

- 不要因为普通进度自动改写长期资料库。
- 只有身份、长期关系、能力体系、世界规则或其他稳定 canon 发生重大变化时，才提出资料库更新建议。
- 如果需要更新长期稳定资料库，先请求用户确认，再执行。

## 最终输出

- 返回最终正文或用户要求的写作产物。
- 只有任务产生了可持久化进展，或用户要求说明时，才附带简短状态更新摘要。
- 除非用户明确要求检查流程，否则隐藏内部角色对话。
