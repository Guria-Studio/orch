package cli

import (
	"bytes"
	"strings"
	"testing"
)

// TestRenderBoxTable covers fixed-width inputs, a value wider than its
// header, and zero rows (header + borders only, no data row).
func TestRenderBoxTable(t *testing.T) {
	cases := []struct {
		name    string
		headers []string
		rows    []boxTableRow
		want    string
	}{
		{
			name:    "fixed-width inputs",
			headers: []string{"ID", "STATUS"},
			rows: []boxTableRow{
				{cells: []string{"F1.T1", "done"}},
				{cells: []string{"F1.T2", "todo"}},
			},
			want: strings.Join([]string{
				"┌───────┬────────┐",
				"│ ID    │ STATUS │",
				"├───────┼────────┤",
				"│ F1.T1 │ done   │",
				"│ F1.T2 │ todo   │",
				"└───────┴────────┘",
			}, "\n") + "\n",
		},
		{
			name:    "a value longer than its header widens the column",
			headers: []string{"ID", "STATUS"},
			rows: []boxTableRow{
				{cells: []string{"F1.T1", "blocked-by-budget:claude until 2026-11-13T18:39:35Z"}},
			},
			want: strings.Join([]string{
				"┌───────┬─────────────────────────────────────────────────────┐",
				"│ ID    │ STATUS                                              │",
				"├───────┼─────────────────────────────────────────────────────┤",
				"│ F1.T1 │ blocked-by-budget:claude until 2026-11-13T18:39:35Z │",
				"└───────┴─────────────────────────────────────────────────────┘",
			}, "\n") + "\n",
		},
		{
			name:    "zero rows still renders header and borders",
			headers: []string{"ID", "STATUS"},
			rows:    nil,
			want: strings.Join([]string{
				"┌────┬────────┐",
				"│ ID │ STATUS │",
				"├────┼────────┤",
				"└────┴────────┘",
			}, "\n") + "\n",
		},
		{
			name:    "a separatorAbove row draws a mid rule (phase break)",
			headers: []string{"ID", "PHASE"},
			rows: []boxTableRow{
				{cells: []string{"F1.T1", "1"}},
				{cells: []string{"F2.T1", "2"}, separatorAbove: true},
			},
			want: strings.Join([]string{
				"┌───────┬───────┐",
				"│ ID    │ PHASE │",
				"├───────┼───────┤",
				"│ F1.T1 │ 1     │",
				"├───────┼───────┤",
				"│ F2.T1 │ 2     │",
				"└───────┴───────┘",
			}, "\n") + "\n",
		},
		{
			name:    "multi-byte cell values are measured in runes, not bytes",
			headers: []string{"ID", "NOTE"},
			rows: []boxTableRow{
				{cells: []string{"F1.T1", "café"}},
			},
			// "café" is 4 runes / 5 bytes; the column must be sized to 4,
			// matching "NOTE"'s width, not widened by the extra UTF-8 byte.
			want: strings.Join([]string{
				"┌───────┬──────┐",
				"│ ID    │ NOTE │",
				"├───────┼──────┤",
				"│ F1.T1 │ café │",
				"└───────┴──────┘",
			}, "\n") + "\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := renderBoxTable(&buf, tc.headers, tc.rows); err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != tc.want {
				t.Errorf("renderBoxTable() =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

// TestIsTerminalWriter: only an *os.File backs the TTY check (same pattern
// as `orch tui`, tui.go); anything else — a bytes.Buffer, any other
// io.Writer — reads as "not a terminal".
func TestIsTerminalWriter(t *testing.T) {
	var buf bytes.Buffer
	if isTerminalWriter(&buf) {
		t.Error("a bytes.Buffer must not read as a terminal")
	}
}

// TestUseBoxTable is the fallback decision #619's acceptance criteria rest
// on: --plain always wins; absent it, the box table only shows on a real
// terminal.
func TestUseBoxTable(t *testing.T) {
	cases := []struct {
		name  string
		plain bool
		isTTY bool
		want  bool
	}{
		{"TTY, no --plain: box table", false, true, true},
		{"TTY, --plain: forces the old plain layout", true, true, false},
		{"non-TTY, no --plain: falls back to the old plain layout", false, false, false},
		{"non-TTY, --plain: still plain", true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := useBoxTable(tc.plain, tc.isTTY); got != tc.want {
				t.Errorf("useBoxTable(%v, %v) = %v, want %v", tc.plain, tc.isTTY, got, tc.want)
			}
		})
	}
}
