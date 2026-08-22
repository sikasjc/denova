package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

const maxPendingEditFileArgumentsBytes = 512 << 10

// prepareEditFileArguments handles only the recoverable shape error where an
// edit_file request contains its patch but omits file_path. It intentionally
// does not guess a path from workspace files: the model must provide it.
func prepareEditFileArguments(toolName, arguments string, observer *RunObserver) (string, string) {
	if normalizeToolName(toolName) != "edit_file" || observer == nil {
		return arguments, ""
	}

	payload, ok := decodeJSONObject(arguments)
	if !ok {
		return arguments, ""
	}
	path := jsonString(payload["file_path"])
	if path == "" && hasEditPayload(payload) {
		if len(arguments) > maxPendingEditFileArgumentsBytes {
			return arguments, ""
		}
		// A missing optional file_revision must not be serialized as JSON null;
		// retaining only the fields that were present keeps the repaired call
		// equivalent to the original call.
		cachedPayload := map[string]json.RawMessage{"edits": payload["edits"]}
		if revision, exists := payload["file_revision"]; exists {
			cachedPayload["file_revision"] = revision
		}
		cached, err := json.Marshal(cachedPayload)
		if err != nil || len(cached) > maxPendingEditFileArgumentsBytes {
			return arguments, ""
		}
		observer.RememberPendingEditFile(string(cached))
		return arguments, missingEditFilePathMessage()
	}

	if path == "" || !isPathOnlyEditFileRepair(payload) {
		return arguments, ""
	}
	cached, ok := observer.TakePendingEditFile()
	if !ok {
		return arguments, ""
	}
	return mergePendingEditFileArguments(cached, path, payload), ""
}

func decodeJSONObject(arguments string) (map[string]json.RawMessage, bool) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil || payload == nil {
		return nil, false
	}
	return payload, true
}

func jsonString(value json.RawMessage) string {
	var decoded string
	if json.Unmarshal(value, &decoded) != nil {
		return ""
	}
	return strings.TrimSpace(decoded)
}

func hasEditPayload(payload map[string]json.RawMessage) bool {
	edits, ok := payload["edits"]
	if !ok {
		return false
	}
	var items []json.RawMessage
	return json.Unmarshal(edits, &items) == nil && len(items) > 0
}

func isPathOnlyEditFileRepair(payload map[string]json.RawMessage) bool {
	for key := range payload {
		if key != "file_path" {
			return false
		}
	}
	return true
}

func mergePendingEditFileArguments(cached, path string, _ map[string]json.RawMessage) string {
	base, ok := decodeJSONObject(cached)
	if !ok {
		return fmt.Sprintf(`{"file_path":%q}`, path)
	}
	pathJSON, _ := json.Marshal(path)
	base["file_path"] = pathJSON
	merged, err := json.Marshal(base)
	if err != nil {
		return fmt.Sprintf(`{"file_path":%q}`, path)
	}
	return string(merged)
}

func missingEditFilePathMessage() string {
	return `[tool error]
type: recoverable_missing_argument
tool: edit_file
missing: file_path
cached: true
retryable: true
workspace_mutated: false

中文：本次 edit_file 的 edits 和 file_revision 已在当前运行中缓存。下一次只需调用 edit_file 并传入 file_path，不需要重新生成 edits 或 old_string/new_string。
请下一次只传入 file_path，Denova 会自动合并已缓存的编辑参数。`
}
