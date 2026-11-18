package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// boxTableRow is one data row of a box-drawn table. separatorAbove draws a
// ├─┼─┤ rule before the row — used to mark a phase boundary (#619's "nice to
// have": an optional separator between phases).
type boxTableRow struct {
	cells          []string
	separatorAbove bool
}

// renderBoxTable writes headers and rows as a Unicode box-drawing table:
//
//	┌─────────┬─────────┐
//	│ HEADER  │ HEADER  │
//	├─────────┼─────────┤
//	│ value   │ value   │
//	└─────────┴─────────┘
//
// Column widths are measured in runes, not bytes, so a multi-byte cell value
// (anything beyond ASCII) still lines up; the box-drawing characters
// themselves are UTF-8 but outside any width computation. Zero rows still
// renders header + borders, just no data row.
func renderBoxTable(w io.Writer, headers []string, rows []boxTableRow) error {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, r := range rows {
		for i, c := range r.cells {
			if i >= len(widths) {
				continue
			}
			if n := utf8.RuneCountInString(c); n > widths[i] {
				widths[i] = n
			}
		}
	}

	border := func(left, mid, right string) string {
		segs := make([]string, len(widths))
		for i, width := range widths {
			segs[i] = strings.Repeat("─", width+2)
		}
		return left + strings.Join(segs, mid) + right
	}
	dataRow := func(cells []string) string {
		segs := make([]string, len(widths))
		for i, width := range widths {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			pad := width - utf8.RuneCountInString(cell)
			if pad < 0 {
				pad = 0
			}
			segs[i] = " " + cell + strings.Repeat(" ", pad) + " "
		}
		return "│" + strings.Join(segs, "│") + "│"
	}

	lines := make([]string, 0, len(rows)+4)
	lines = append(lines, border("┌", "┬", "┐"), dataRow(headers), border("├", "┼", "┤"))
	for _, r := range rows {
		if r.separatorAbove {
			lines = append(lines, border("├", "┼", "┤"))
		}
		lines = append(lines, dataRow(r.cells))
	}
	lines = append(lines, border("└", "┴", "┘"))

	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// useBoxTable decides, in one place, whether `orch status` draws the
// box-drawing table or falls back to the historic plain layout (#619):
// `--plain` always wins, otherwise it follows whether stdout is a terminal.
// Factored out so the decision itself is unit-testable without a real TTY.
func useBoxTable(plain, isTTY bool) bool {
	return !plain && isTTY
}

// isTerminalWriter reports whether w is a terminal orch can draw borders on.
// Same pattern as `orch tui` (tui.go): only an *os.File's fd can be checked,
// so anything else (a bytes.Buffer in a test, a pipe wrapped by cobra) reads
// as "not a terminal" and gets the plain layout.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd())) // #nosec G115 -- a file descriptor fits in an int
}
