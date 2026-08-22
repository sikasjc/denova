package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// compactWriteTodosInput deliberately reuses Deep Agent's public TODO type and
// session key. It replaces only the upstream coding-specific tool description,
// which otherwise costs almost 10 KiB in every writing-model request.
type compactWriteTodosInput struct {
	Todos []deep.TODO `json:"todos" jsonschema:"required,description=本轮待办列表；每项包含 content、activeForm 和 pending/in_progress/completed 状态"`
}

func newCompactWriteTodosTool() (tool.BaseTool, error) {
	return utils.InferTool("write_todos", "更新本轮待办列表，用于展示和跟踪复杂写作任务的进度。", func(ctx context.Context, input compactWriteTodosInput) (string, error) {
		adk.AddSessionValue(ctx, deep.SessionKeyTodos, input.Todos)
		return fmt.Sprintf("已更新 %d 项待办。", len(input.Todos)), nil
	})
}
