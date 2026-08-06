package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"

	"denova/config"
)

const maxWritingIntentClassifierInputBytes = 256 * 1024

type writingIntentClassifierPayload struct {
	Intent     string `json:"intent"`
	ExecuteNow bool   `json:"execute_now"`
	Confidence string `json:"confidence"`
	Evidence   string `json:"evidence,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type WritingIntentClassification struct {
	Intent     config.WritingIntent
	ExecuteNow bool
	Confidence string
	Evidence   string
	Reason     string
}

func (c WritingIntentClassification) AllowsExecution(message string) bool {
	if !c.ExecuteNow || c.Confidence != "high" ||
		(c.Intent != config.WritingIntentProseGeneration && c.Intent != config.WritingIntentProseRevision) {
		return false
	}
	evidence := strings.TrimSpace(c.Evidence)
	return evidence != "" && strings.Contains(message, evidence)
}

// ClassifyWritingIntent routes one free-form Writing message through the
// configured fast profile. The classifier sees the current message only;
// history must not turn a discussion request into an execution request.
func ClassifyWritingIntent(ctx context.Context, cfg *config.Config, message string) (WritingIntentClassification, error) {
	if cfg == nil {
		return WritingIntentClassification{}, fmt.Errorf("配置不存在")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return WritingIntentClassification{}, fmt.Errorf("写作意图分类输入为空")
	}
	if len([]byte(message)) > maxWritingIntentClassifierInputBytes {
		return WritingIntentClassification{}, fmt.Errorf("写作意图分类输入超过 %d bytes", maxWritingIntentClassifierInputBytes)
	}

	classifierCfg := writingIntentClassifierConfig(cfg)
	modelCfg := chatModelConfigForAgent(&classifierCfg, config.AgentKindToolAgent)
	system := `你是 Denova 写作模式的保守意图分类器。只判断“用户这一条消息现在是否要求执行正文写作或正文修改”，不要参考历史，也不要执行任务。

只允许输出 JSON：
{"intent":"discussion|planning|prose_generation|prose_revision","execute_now":true|false,"confidence":"high|medium|low","evidence":"当前用户消息中的原文短句","reason":"一句短理由"}

判定规则：
- discussion：讨论方向、征求建议、询问怎么写/要不要写、评价方案、尚未授权执行。
- planning：只要求大纲、细纲、设定、计划或方案，不要求写正文。
- prose_generation：明确要求现在生成、续写或落盘小说正文。
- prose_revision：明确要求现在改写、修订、润色或应用意见到正文。
- 提到“写作、续写、改写、创作”等词不等于授权执行；疑问句、讨论句、方案比较默认 discussion。
- 只有当前消息存在明确执行命令时，才能令 execute_now=true，并把支持执行的原文短句逐字复制到 evidence。
- discussion / planning 必须令 execute_now=false；无法确定时输出 discussion + low + execute_now=false。
- evidence 必须是当前用户消息的连续原文；不要改写、概括或引用历史。`
	messages := []*schema.Message{
		schema.SystemMessage(protectedSystemInstruction(&classifierCfg, config.AgentKindToolAgent, system)),
		schema.UserMessage(message),
	}
	content, err := generateWithJSONFallback(
		ctx,
		modelCfg,
		messages,
		config.AgentKindToolAgent,
		"writing_intent_classification",
		"writing-intent-classifier",
	)
	if err != nil {
		return WritingIntentClassification{}, err
	}
	return parseWritingIntentClassification(content)
}

func writingIntentClassifierConfig(cfg *config.Config) config.Config {
	classifierCfg := *cfg
	classifierCfg.AgentPrompts = config.AgentPromptSettings{}
	classifierCfg.AgentModels.Default = config.AgentModelOverride{}
	classifierCfg.AgentModels.ToolAgent = config.AgentModelOverride{
		ProfileID:      config.NormalizeFastModelProfileID(cfg.WritingComputeFastProfileID),
		EnableThinking: boolPointer(false),
	}
	return classifierCfg
}

func boolPointer(value bool) *bool {
	return &value
}

func parseWritingIntentClassification(content string) (WritingIntentClassification, error) {
	var payload writingIntentClassifierPayload
	if err := json.Unmarshal([]byte(extractJSONContent(content)), &payload); err != nil {
		return WritingIntentClassification{}, fmt.Errorf("解析写作意图分类结果失败: %w", err)
	}
	classification := WritingIntentClassification{
		ExecuteNow: payload.ExecuteNow,
		Confidence: strings.ToLower(strings.TrimSpace(payload.Confidence)),
		Evidence:   strings.TrimSpace(payload.Evidence),
		Reason:     strings.TrimSpace(payload.Reason),
	}
	switch strings.ToLower(strings.TrimSpace(payload.Intent)) {
	case "discussion":
		classification.Intent = config.WritingIntentAnalysis
	case "planning":
		classification.Intent = config.WritingIntentPlanning
	case "prose_generation":
		classification.Intent = config.WritingIntentProseGeneration
	case "prose_revision":
		classification.Intent = config.WritingIntentProseRevision
	default:
		return WritingIntentClassification{}, fmt.Errorf("未知写作意图分类: %s", payload.Intent)
	}
	switch classification.Confidence {
	case "high", "medium", "low":
	default:
		return WritingIntentClassification{}, fmt.Errorf("未知写作意图置信度: %s", payload.Confidence)
	}
	return classification, nil
}
