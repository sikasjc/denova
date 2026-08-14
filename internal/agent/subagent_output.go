package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type requiredOutputAgent struct {
	agent adk.Agent
}

func (a requiredOutputAgent) Name(ctx context.Context) string {
	return a.agent.Name(ctx)
}

func (a requiredOutputAgent) Description(ctx context.Context) string {
	return a.agent.Description(ctx)
}

func (a requiredOutputAgent) Run(ctx context.Context, input *adk.AgentInput, opts ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()

		inner := a.agent.Run(ctx, input, opts...)
		var lastAssistantContent string
		var sawAssistantOutput bool
		for {
			event, ok := inner.Next()
			if !ok {
				break
			}
			if event == nil {
				continue
			}
			if event.Err != nil {
				generator.Send(event)
				return
			}
			if output := event.Output; output != nil && output.MessageOutput != nil {
				messageOutput := output.MessageOutput
				if messageOutput.IsStreaming {
					message, err := messageOutput.GetMessage()
					if err != nil {
						generator.Send(&adk.AgentEvent{Err: fmt.Errorf("读取 %s 输出失败: %w", a.Name(ctx), err)})
						return
					}
					messageOutput.IsStreaming = false
					messageOutput.Message = message
					messageOutput.MessageStream = nil
				}
				if isAssistantMessageOutput(messageOutput) {
					sawAssistantOutput = true
					if messageOutput.Message != nil {
						lastAssistantContent = messageOutput.Message.Content
					}
				}
			}
			generator.Send(event)
		}

		if !sawAssistantOutput || strings.TrimSpace(lastAssistantContent) == "" {
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("%s 返回空结果；请直接输出最终审稿报告，不要只返回 thinking 或工具调用", a.Name(ctx))})
		}
	}()
	return iterator
}

func isAssistantMessageOutput(output *adk.MessageVariant) bool {
	if output == nil {
		return false
	}
	if output.Role == schema.Tool {
		return false
	}
	if output.Message != nil && output.Message.Role == schema.Tool {
		return false
	}
	return true
}
