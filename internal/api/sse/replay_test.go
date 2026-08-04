package sse

import (
	"reflect"
	"testing"

	"denova/internal/agent"
)

func TestCoalesceReplaySnapshotMergesConsecutiveChunks(t *testing.T) {
	snapshot := []agent.Event{
		{Type: "chunk", Data: map[string]interface{}{"content": "第一", "run_id": "run-1", "subagent": false}},
		{Type: "chunk", Data: map[string]interface{}{"content": "段", "run_id": "run-1", "subagent": false}},
		{Type: "chunk", Data: map[string]interface{}{"content": "内容", "run_id": "run-1", "subagent": false}},
	}
	got := coalesceReplaySnapshot(snapshot)
	if len(got) != 1 {
		t.Fatalf("expected 1 merged event, got %d", len(got))
	}
	data, _ := eventDataMap(got[0].Data)
	if data["content"] != "第一段内容" {
		t.Fatalf("expected merged content, got %q", data["content"])
	}
	if data["run_id"] != "run-1" {
		t.Fatalf("expected run_id preserved, got %q", data["run_id"])
	}
}

func TestCoalesceReplaySnapshotDoesNotMergeAcrossDifferentSources(t *testing.T) {
	snapshot := []agent.Event{
		{Type: "chunk", Data: map[string]interface{}{"content": "a", "run_id": "run-1"}},
		{Type: "chunk", Data: map[string]interface{}{"content": "b", "run_id": "run-2"}},
	}
	got := coalesceReplaySnapshot(snapshot)
	if len(got) != 2 {
		t.Fatalf("expected 2 events for different run_id, got %d", len(got))
	}
}

func TestCoalesceReplaySnapshotKeepsStructuralEventsAsBoundaries(t *testing.T) {
	snapshot := []agent.Event{
		{Type: "chunk", Data: map[string]interface{}{"content": "前"}},
		{Type: "tool_call", Data: map[string]interface{}{"id": "t1", "name": "write_file"}},
		{Type: "chunk", Data: map[string]interface{}{"content": "后"}},
	}
	got := coalesceReplaySnapshot(snapshot)
	if len(got) != 3 {
		t.Fatalf("expected structural event to break coalescing, got %d events", len(got))
	}
	if got[1].Type != "tool_call" {
		t.Fatalf("expected tool_call preserved at index 1, got %s", got[1].Type)
	}
}

func TestCoalesceReplaySnapshotMergesToolArgsDeltaByTool(t *testing.T) {
	snapshot := []agent.Event{
		{Type: "tool_args_delta", Data: map[string]interface{}{"id": "t1", "delta": `{"path"`}},
		{Type: "tool_args_delta", Data: map[string]interface{}{"id": "t1", "delta": `:"a.md"}`}},
		{Type: "tool_args_delta", Data: map[string]interface{}{"id": "t2", "delta": `{"x":1}`}},
	}
	got := coalesceReplaySnapshot(snapshot)
	if len(got) != 2 {
		t.Fatalf("expected 2 merged deltas (one per tool), got %d", len(got))
	}
	first, _ := eventDataMap(got[0].Data)
	if first["delta"] != `{"path":"a.md"}` {
		t.Fatalf("expected merged delta for t1, got %q", first["delta"])
	}
}

func TestCoalesceReplaySnapshotDoesNotMutateInput(t *testing.T) {
	original := map[string]interface{}{"content": "第一", "run_id": "run-1"}
	snapshot := []agent.Event{
		{Type: "chunk", Data: original},
		{Type: "chunk", Data: map[string]interface{}{"content": "段", "run_id": "run-1"}},
	}
	coalesceReplaySnapshot(snapshot)
	if !reflect.DeepEqual(original, map[string]interface{}{"content": "第一", "run_id": "run-1"}) {
		t.Fatalf("coalescing must not mutate input snapshot data, got %v", original)
	}
}

func TestCoalesceReplaySnapshotMergesMapStringData(t *testing.T) {
	snapshot := []agent.Event{
		{Type: "thinking", Data: map[string]string{"content": "逐帧"}},
		{Type: "thinking", Data: map[string]string{"content": "思考"}},
	}
	got := coalesceReplaySnapshot(snapshot)
	if len(got) != 1 {
		t.Fatalf("expected 1 merged thinking event, got %d", len(got))
	}
	data, _ := eventDataMap(got[0].Data)
	if data["content"] != "逐帧思考" {
		t.Fatalf("expected merged thinking content, got %q", data["content"])
	}
}
