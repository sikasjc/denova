package config

// DefaultChoreographySubAgents returns the built-in read-only specialists used
// by choreography router Skills. User and workspace layers may override or
// disable them by stable ID.
func DefaultChoreographySubAgents() []SubAgentConfig {
	enabled := true
	disabled := false
	readOnlyTools := AgentToolOverride{
		FileWrite:       &disabled,
		LoreWrite:       &disabled,
		Todo:            &disabled,
		WebSearch:       &disabled,
		ImageGeneration: &disabled,
		ShellExecute:    &disabled,
		Skills:          &enabled,
	}
	return []SubAgentConfig{
		{
			ID:          "choreographer",
			Name:        "动作编排",
			Description: "专业动作编排 Agent。把场景描述推演成结构化多主体动作节拍表；当 choreography Skill 要求委派时使用。",
			Enabled:     &enabled,
			Parents:     []string{AgentKindIDE, AgentKindInteractiveStory},
			SystemPrompt: `你是多主体动作编排 SubAgent。先调用 skill 工具加载 action-choreography，然后严格执行完整方法。
只返回 STAGE + SUBJECTS + BEATS 节拍表，不写正文、不改文件、不扩写；必要时读取有限的作品上下文用于连续性判断。
若输入缺少会实质改变因果链的信息，返回 [BLOCKER] 和需要父 Agent 确认的最小问题。`,
			Tools: readOnlyTools,
		},
		{
			ID:          "intimacy-choreographer",
			Name:        "亲密编排",
			Description: "专业亲密动作编排 Agent。把场景描述推演成结构化肢体、情绪、回应与张力节拍表；当 choreography Skill 要求委派时使用。",
			Enabled:     &enabled,
			Parents:     []string{AgentKindIDE, AgentKindInteractiveStory},
			SystemPrompt: `你是亲密动作编排 SubAgent。先调用 skill 工具加载 intimacy-choreography，然后严格执行完整方法。
只返回 STAGE + SUBJECTS + BEATS 节拍表，不写正文、不改文件、不扩写；尺度完全由父 Agent 的原始输入决定，不自行升级或净化。
所有主体必须成年且自愿；存在冲突、不明确、迟疑、退缩或暂停信号时，优先返回 [BLOCKER] 或在后续拍明确回应。`,
			Tools: readOnlyTools,
		},
	}
}
