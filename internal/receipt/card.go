package receipt

import (
	"fmt"
	"html"
	"strings"

	"github.com/Guria-Studio/orch/internal/state"
)

// Card sizes: the Open Graph image size, which X, LinkedIn and Slack all
// show uncropped.
const (
	CardWidth  = 1200
	CardHeight = 630
)

const (
	cardSans = `Inter, -apple-system, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif`
	cardMono = `ui-monospace, 'SF Mono', Menlo, Consolas, monospace`
)

// SVG renders the receipt as a picture to post: the run's headline numbers
// on one card, with the "Built with orch" mark unless builtWith is false.
// Hand-written SVG with system fonts, so neither orch nor the page that turns
// it into a PNG needs an image library.
func (r *Receipt) SVG(project string, builtWith bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">`+"\n",
		CardWidth, CardHeight, CardWidth, CardHeight)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#09090b"/>`+"\n", CardWidth, CardHeight)
	// The orch mark: site/assets/icon.svg's four bars.
	b.WriteString(`<g transform="translate(72 64) scale(0.16)">` +
		`<rect x="0" y="0" width="262" height="30" rx="15" fill="#7c3aed"/>` +
		`<rect x="0" y="38" width="194" height="30" rx="15" fill="#a78bfa"/>` +
		`<rect x="0" y="76" width="290" height="30" rx="15" fill="#7c3aed"/>` +
		`<rect x="0" y="114" width="162" height="30" rx="15" fill="#a78bfa"/></g>` + "\n")
	text(&b, 128, 86, 26, "#fafafa", cardSans, "600", "start", "orch")
	text(&b, CardWidth-72, 86, 20, "#71717a", cardMono, "400", "end", cut(r.RunID, 40))

	text(&b, 72, 196, 56, "#fafafa", cardSans, "700", "start", cut(project, 34))
	when := "still running"
	if t, ok := state.ParseTS(r.FinishedAt); ok && r.Finished {
		when = "finished " + t.Format("Jan 2, 2006")
	}
	text(&b, 72, 244, 26, "#a1a1aa", cardSans, "400", "start", when)

	stat := func(col int, value, label string) {
		x := 72 + col*270
		text(&b, x, 372, 64, "#fafafa", cardSans, "700", "start", value)
		text(&b, x, 412, 24, "#a1a1aa", cardSans, "400", "start", label)
	}
	stat(0, fmt.Sprint(len(r.Done)), noun(len(r.Done), "task done", "tasks done"))
	if len(r.PRs) > 0 {
		passed := 0
		for _, p := range r.PRs {
			if p.CIStatus == "success" {
				passed++
			}
		}
		stat(1, fmt.Sprint(len(r.PRs)), fmt.Sprintf("%s, %d passed CI", noun(len(r.PRs), "PR", "PRs"), passed))
	} else {
		stat(1, fmt.Sprint(r.Dispatches), noun(r.Dispatches, "agent run", "agent runs"))
	}
	stat(2, Duration(r.WallSeconds), "wall time")
	stat(3, r.costLabel(), "spent")

	var more []string
	more = append(more, Duration(r.AgentSeconds)+" of agent time")
	if n := len(r.Blocked); n > 0 {
		more = append(more, fmt.Sprintf("%d blocked", n))
	}
	if r.FailedAttempts > 0 {
		more = append(more, plural(r.FailedAttempts, "failed attempt"))
	}
	text(&b, 72, 484, 24, "#a1a1aa", cardSans, "400", "start", strings.Join(more, " · "))
	if len(r.Providers) > 0 {
		names := make([]string, len(r.Providers))
		for i, p := range r.Providers {
			names[i] = p.Provider
		}
		text(&b, 72, 524, 22, "#71717a", cardMono, "400", "start", cut(strings.Join(names, " · "), 80))
	}

	if builtWith {
		fmt.Fprintf(&b, `<rect x="0" y="%d" width="%d" height="4" fill="#7c3aed"/>`+"\n", CardHeight-4, CardWidth)
		text(&b, CardWidth-72, 584, 22, "#a78bfa", cardSans, "500", "end", "Built with orch · github.com/Guria-Studio/orch")
	}
	b.WriteString("</svg>\n")
	return b.String()
}

// costLabel is the run's total: "$4.10" when a CLI reported any of it,
// "~$0.40" when all of it is the pricing.yaml estimate, "—" with no data.
func (r *Receipt) costLabel() string {
	switch {
	case r.TotalCostUSD <= 0:
		return "—"
	case r.EstimatedCostUSD >= r.TotalCostUSD:
		return fmt.Sprintf("~$%.2f", r.TotalCostUSD)
	}
	return fmt.Sprintf("$%.2f", r.TotalCostUSD)
}

func text(b *strings.Builder, x, y, size int, fill, family, weight, anchor, s string) {
	fmt.Fprintf(b, `<text x="%d" y="%d" font-family="%s" font-size="%d" font-weight="%s" fill="%s" text-anchor="%s">%s</text>`+"\n",
		x, y, family, size, weight, fill, anchor, html.EscapeString(s))
}

// cut keeps a line inside the card: n runes, the last one an ellipsis.
func cut(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func noun(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
