package sse

import (
	"reflect"

	"denova/internal/agent"
)

// coalesceReplaySnapshot 合并回放快照中连续、同源的流式增量事件，把"每个 token 一个事件"
// 塌缩为"每段一个事件"。这是无损压缩：合并后重建的内容与逐条回放完全一致，只是事件对象数、
// SSE 帧数与前端迭代次数大幅下降，避免超长输出中断后重连（resume）时回放十万级事件打爆
// 主线程与内存。
//
// 仅用于回放快照，不触碰实时事件流；因此实时输出的节奏与行为保持不变。
//
// 合并条件严格：仅当相邻事件类型相同、且除累积文本字段外的所有数据字段完全相等时才合并，
// 保证 run_id / 工具 id / 子 Agent 元数据等不会被跨源错误拼接。
func coalesceReplaySnapshot(snapshot []agent.Event) []agent.Event {
	if len(snapshot) < 2 {
		return snapshot
	}
	out := make([]agent.Event, 0, len(snapshot))
	for _, ev := range snapshot {
		field := coalescibleTextField(ev.Type)
		if field == "" {
			out = append(out, ev)
			continue
		}
		curMap, ok := eventDataMap(ev.Data)
		if !ok {
			out = append(out, ev)
			continue
		}
		if len(out) > 0 {
			prev := &out[len(out)-1]
			if prev.Type == ev.Type {
				if prevMap, prevOK := eventDataMap(prev.Data); prevOK && sameExceptField(prevMap, curMap, field) {
					// prev.Data 已是本函数写入的克隆，可安全原地更新累积字段。
					prevMap[field] = dataString(prevMap[field]) + dataString(curMap[field])
					prev.Data = prevMap
					continue
				}
			}
		}
		// 克隆后再入栈，避免后续合并写入污染 task 的实时事件缓冲（快照与实时缓冲共享 map 引用）。
		out = append(out, agent.Event{Type: ev.Type, Data: cloneEventDataMap(curMap)})
	}
	return out
}

// coalescibleTextField 返回某事件类型可累积拼接的数据字段；返回空字符串表示该类型不参与合并。
func coalescibleTextField(eventType string) string {
	switch eventType {
	case "chunk", "thinking":
		return "content"
	case "tool_args_delta":
		return "delta"
	default:
		return ""
	}
}

// eventDataMap 把事件 Data 归一化为 map[string]interface{}；无法识别的形态返回 ok=false。
func eventDataMap(data interface{}) (map[string]interface{}, bool) {
	switch typed := data.(type) {
	case map[string]interface{}:
		return typed, true
	case map[string]string:
		out := make(map[string]interface{}, len(typed))
		for k, v := range typed {
			out[k] = v
		}
		return out, true
	default:
		return nil, false
	}
}

// sameExceptField 判断两个数据 map 除累积字段外是否完全一致。
func sameExceptField(a, b map[string]interface{}, field string) bool {
	for k, va := range a {
		if k == field {
			continue
		}
		vb, ok := b[k]
		if !ok || !reflect.DeepEqual(va, vb) {
			return false
		}
	}
	for k := range b {
		if k == field {
			continue
		}
		if _, ok := a[k]; !ok {
			return false
		}
	}
	return true
}

func dataString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func cloneEventDataMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
