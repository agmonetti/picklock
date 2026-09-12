package screens_test

import (
	"testing"

	"github.com/agmonetti/picklock/internal/tui/screens"
)

func TestExtractTextRange(t *testing.T) {
	text := "SELECT id, name\nFROM users\nWHERE active = true;"

	cases := []struct {
		name      string
		startLine int
		startCol  int
		endLine   int
		endCol    int
		want      string
	}{
		{
			name:      "empty text",
			startLine: 0, startCol: 0, endLine: 1, endCol: 1,
			want: "",
		},
		{
			name:      "single line range",
			startLine: 0, startCol: 0, endLine: 0, endCol: 6,
			want: "SELECT",
		},
		{
			name:      "single line range reversed",
			startLine: 0, startCol: 6, endLine: 0, endCol: 0,
			want: "SELECT",
		},
		{
			name:      "single line mid range",
			startLine: 0, startCol: 7, endLine: 0, endCol: 15,
			want: "id, name",
		},
		{
			name:      "multiline range",
			startLine: 0, startCol: 7, endLine: 1, endCol: 4,
			want: "id, name\nFROM",
		},
		{
			name:      "multiline range reversed",
			startLine: 1, startCol: 4, endLine: 0, endCol: 7,
			want: "id, name\nFROM",
		},
		{
			name:      "all 3 lines",
			startLine: 0, startCol: 0, endLine: 2, endCol: 20,
			want: "SELECT id, name\nFROM users\nWHERE active = true;",
		},
		{
			name:      "out of bounds clamp",
			startLine: -5, startCol: -2, endLine: 50, endCol: 100,
			want: "SELECT id, name\nFROM users\nWHERE active = true;",
		},
		{
			name:      "same point",
			startLine: 1, startCol: 2, endLine: 1, endCol: 2,
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := text
			if tc.name == "empty text" {
				input = ""
			}
			got := screens.ExtractTextRange(input, tc.startLine, tc.startCol, tc.endLine, tc.endCol)
			if got != tc.want {
				t.Errorf("ExtractTextRange() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEditorGutterWidth(t *testing.T) {
	if w := screens.EditorGutterWidth(1); w != 3 {
		t.Errorf("EditorGutterWidth(1) = %d, want 3", w)
	}
	if w := screens.EditorGutterWidth(9); w != 3 {
		t.Errorf("EditorGutterWidth(9) = %d, want 3", w)
	}
	if w := screens.EditorGutterWidth(10); w != 4 {
		t.Errorf("EditorGutterWidth(10) = %d, want 4", w)
	}
	if w := screens.EditorGutterWidth(100); w != 5 {
		t.Errorf("EditorGutterWidth(100) = %d, want 5", w)
	}
}
