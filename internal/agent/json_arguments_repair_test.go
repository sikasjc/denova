package agent

import "testing"

func TestRepairToolArgumentsJSONAddsOnlyMissingClosers(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments string
		want      string
		repaired  bool
	}{
		{
			name:      "object",
			arguments: `{"file_path":"draft.md"`,
			want:      `{"file_path":"draft.md"}`,
			repaired:  true,
		},
		{
			name:      "nested array",
			arguments: `{"edits":[{"old_string":"a","new_string":"b"}]`,
			want:      `{"edits":[{"old_string":"a","new_string":"b"}]}`,
			repaired:  true,
		},
		{
			name:      "unterminated string",
			arguments: `{"content":"正文`,
			want:      `{"content":"正文`,
			repaired:  false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, repaired := repairToolArgumentsJSON(test.arguments)
			if got != test.want || repaired != test.repaired {
				t.Fatalf("repairToolArgumentsJSON() = %q, %v; want %q, %v", got, repaired, test.want, test.repaired)
			}
		})
	}
}
