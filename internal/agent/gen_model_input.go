package agent

import (
	"context"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// noFormatGenModelInput 将 instruction 与输入消息原样组装为模型输入，不做任何模板插值。
//
// 设计决策：eino adk 的默认 GenModelInput 在 run 上下文里存在 SessionValues 时
// （例如 write_todos 工具写入 todos 之后），会把整个 instruction 当 FString 模板解析；
// 用户自定义 SubAgent 提示词里的字面花括号（如 JSON 示例 {order}）会触发
// "could not find key: ..." 并直接废掉一次子 Agent 调用。
// Denova 的提示词体系不使用 FString 插值（动态上下文由 prompt 组装层在传入 eino
// 之前完成拼接），因此自定义 SubAgent 统一关闭该格式化行为。
//
// 安全性依据：
//  1. 全仓库不调用 WithSessionValues/AddSessionValue/OutputKey；运行期 SessionValues
//     的唯一来源是 eino deep 包内置 write_todos 工具的控制流状态（SessionKeyTodos），
//     并非提示词插值数据源。
//  2. eino deep 包的 root agent 与内置 general sub-agent 本就传入不做格式化的
//     typedGenModelInput——修复前全系统唯一走 FString 默认格式化的就是自定义
//     SubAgent，任何依赖插值的提示词在 root 路径上从未生效过。
//  3. FString 格式化仅在 SessionValues 非空时触发，行为取决于本轮是否调用过
//     write_todos，这种不确定性本身不可依赖；关闭后语义恒定为"原样透传"。
func noFormatGenModelInput(_ context.Context, instruction string, input *adk.AgentInput) ([]*schema.Message, error) {
	msgs := make([]*schema.Message, 0, len(input.Messages)+1)
	if instruction != "" {
		msgs = append(msgs, schema.SystemMessage(instruction))
	}
	msgs = append(msgs, input.Messages...)
	return msgs, nil
}
