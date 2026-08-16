package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// echoGenModelInputChatModel 直接回显收到的系统提示，用于断言模型输入内容。
type echoGenModelInputChatModel struct {
	systemPrompts []string
}

func (m *echoGenModelInputChatModel) Generate(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return schema.SystemMessage(joinModelInputContents(messages)), nil
}

func (m *echoGenModelInputChatModel) Stream(_ context.Context, messages []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{schema.SystemMessage(joinModelInputContents(messages))}), nil
}

func joinModelInputContents(messages []*schema.Message) string {
	var sb strings.Builder
	for _, msg := range messages {
		if msg != nil {
			sb.WriteString(msg.Content)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

const literalBraceInstruction = `输出 JSON 示例：{"order": 1, "items": [{"name": "{order}"}]}
提示词中的 {order} 是字面花括号，不是模板占位符。`

// TestNoFormatGenModelInputKeepsLiteralBraces 验证自定义 GenModelInput
// 在存在 SessionValues 时仍原样保留 instruction。
func TestNoFormatGenModelInputKeepsLiteralBraces(t *testing.T) {
	msgs, err := noFormatGenModelInput(t.Context(), literalBraceInstruction, &adk.AgentInput{
		Messages: []*schema.Message{schema.UserMessage("hello")},
	})
	if err != nil {
		t.Fatalf("noFormatGenModelInput returned error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != schema.System {
		t.Fatalf("expected first message to be system, got %s", msgs[0].Role)
	}
	if msgs[0].Content != literalBraceInstruction {
		t.Fatalf("instruction was modified: %q", msgs[0].Content)
	}
	if msgs[1].Content != "hello" {
		t.Fatalf("expected user message preserved, got %q", msgs[1].Content)
	}
}

// TestSubAgentLiteralBraceInstructionSurvivesSessionValues 复现线上 bug：
// write_todos 等工具写入 SessionValues 后，eino 默认 GenModelInput 会把含
// 字面 {order} 花括号的 SubAgent instruction 当 FString 模板解析并报
// "could not find key: order"。配置 noFormatGenModelInput 后必须成功。
func TestSubAgentLiteralBraceInstructionSurvivesSessionValues(t *testing.T) {
	ctx := t.Context()
	sessionValues := map[string]any{"todos": []any{"draft chapter"}}
	chatModel := &echoGenModelInputChatModel{}

	runAgent := func(genInput adk.GenModelInput) error {
		agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
			Name:          "literal-brace-test",
			Description:   "test",
			Instruction:   literalBraceInstruction,
			Model:         chatModel,
			MaxIterations: 1,
			GenModelInput: genInput,
		})
		if err != nil {
			t.Fatalf("NewChatModelAgent failed: %v", err)
		}
		runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent})
		iterator := runner.Run(ctx, []*schema.Message{schema.UserMessage("开始")},
			adk.WithSessionValues(sessionValues))
		var runErr error
		for {
			event, ok := iterator.Next()
			if !ok {
				break
			}
			if event.Err != nil {
				runErr = event.Err
			}
		}
		return runErr
	}

	// 未修复（nil 走 eino 默认）：必须复现模板解析失败，证明测试场景真实。
	if err := runAgent(nil); err == nil {
		t.Fatal("expected default GenModelInput to fail on literal braces with session values, got nil")
	} else if !strings.Contains(err.Error(), "could not find key") {
		t.Fatalf("expected FString formatting error, got: %v", err)
	}

	// 修复后：instruction 原样进入模型输入。
	if err := runAgent(noFormatGenModelInput); err != nil {
		t.Fatalf("expected noFormatGenModelInput to succeed, got: %v", err)
	}
}
