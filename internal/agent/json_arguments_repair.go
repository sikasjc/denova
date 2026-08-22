package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// repairToolArgumentsJSON repairs only unambiguous missing closing delimiters.
// It never closes an unterminated string or changes string content: those
// cases may mean the model output was truncated in the middle of a write.
func repairToolArgumentsJSON(arguments string) (string, bool) {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" || validateToolArgumentsJSON(trimmed) == nil {
		return arguments, false
	}

	stack := make([]byte, 0, 4)
	inString := false
	escaped := false
	for index := 0; index < len(trimmed); index++ {
		char := trimmed[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == '"' {
				inString = false
			}
			continue
		}

		switch char {
		case '"':
			inString = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) == 0 || stack[len(stack)-1] != char {
				return arguments, false
			}
			stack = stack[:len(stack)-1]
		}
	}
	if inString || escaped || len(stack) == 0 {
		return arguments, false
	}

	var repaired strings.Builder
	repaired.Grow(len(trimmed) + len(stack))
	repaired.WriteString(trimmed)
	for index := len(stack) - 1; index >= 0; index-- {
		repaired.WriteByte(stack[index])
	}
	candidate := repaired.String()
	if validateToolArgumentsJSON(candidate) != nil {
		return arguments, false
	}
	return candidate, true
}

func jsonArgumentsErrorHint(arguments string, err error) string {
	inString, escaped, stack, mismatch, issueOffset := inspectJSONStructure(arguments)
	if inString || escaped {
		return fmt.Sprintf("字符串未闭合（%s附近），检查该位置的引号、反斜杠和换行是否正确转义。", jsonErrorPosition(arguments, issueOffset))
	}
	if mismatch {
		return fmt.Sprintf("JSON 括号不匹配（%s附近），检查 {} 和 []。", jsonErrorPosition(arguments, issueOffset))
	}
	if len(stack) > 0 {
		missing := make([]byte, len(stack))
		for index := range stack {
			missing[len(stack)-1-index] = stack[index]
		}
		return fmt.Sprintf("JSON 缺少闭合符号 %s。", string(missing))
	}
	if strings.Contains(err.Error(), "trailing") || strings.Contains(err.Error(), "after top-level") {
		return fmt.Sprintf("JSON 后存在多余内容（%s附近），检查末尾逗号或多余字符。", jsonErrorPosition(arguments, jsonSyntaxErrorOffset(err)))
	}
	if strings.Contains(err.Error(), "must be a JSON object") {
		return "顶层参数必须是 JSON 对象，不能是数组、字符串或其他类型。"
	}
	return fmt.Sprintf("JSON 格式错误（%s附近），检查逗号、引号和括号。", jsonErrorPosition(arguments, jsonSyntaxErrorOffset(err)))
}

func inspectJSONStructure(arguments string) (inString, escaped bool, stack []byte, mismatch bool, issueOffset int) {
	trimmed := strings.TrimSpace(arguments)
	stringStart := -1
	for index := 0; index < len(trimmed); index++ {
		char := trimmed[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == '"' {
				inString = false
				stringStart = -1
			}
			continue
		}
		switch char {
		case '"':
			inString = true
			stringStart = index
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) == 0 || stack[len(stack)-1] != char {
				return inString, escaped, stack, true, index + 1
			}
			stack = stack[:len(stack)-1]
		}
	}
	if inString {
		return inString, escaped, stack, false, stringStart + 1
	}
	return inString, escaped, stack, false, 0
}

func jsonSyntaxErrorOffset(err error) int {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) && syntaxErr.Offset > 0 {
		return int(syntaxErr.Offset)
	}
	return 0
}

func jsonErrorPosition(arguments string, offset int) string {
	trimmed := strings.TrimSpace(arguments)
	if offset <= 0 {
		offset = len(trimmed) + 1
	}
	if offset > len(trimmed)+1 {
		offset = len(trimmed) + 1
	}
	line, column := 1, 1
	for index := 0; index < offset-1 && index < len(trimmed); index++ {
		if trimmed[index] == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return fmt.Sprintf("第 %d 行第 %d 列", line, column)
}
