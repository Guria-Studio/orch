package receipt

import (
	"encoding/xml"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*.svg from the current renderer")

func cardReceipt() *Receipt {
	return &Receipt{
		RunID: "run-20260915-0800", StartedAt: "2026-09-15T08:00:00Z", FinishedAt: "2026-09-15T10:05:00+00:00", Finished: true,
		WallSeconds: 7500, AgentSeconds: 3000, Dispatches: 4, FailedAttempts: 1, Retries: 1,
		Done:    []TaskRef{{TaskID: "A", Title: "Alpha"}, {TaskID: "B", Title: "Beta"}},
		Blocked: []TaskRef{{TaskID: "C", Title: "Gamma"}},
		Providers: []Provider{
			{Provider: "claude", Dispatches: 3, CostUSD: 1.25, CostSource: "reported"},
			{Provider: "codex", Dispatches: 1, CostUSD: 0.4, EstimatedCostUSD: 0.4, CostSource: "estimated"},
		},
		TotalCostUSD: 1.65, EstimatedCostUSD: 0.4,
		PRs: []PR{{TaskID: "A", URL: "https://github.com/x/y/pull/7", CIStatus: "success"}, {TaskID: "B", URL: "https://github.com/x/y/pull/8", CIStatus: "failure"}},
	}
}

func TestCardGolden(t *testing.T) {
	got := cardReceipt().SVG("acme-billing-api", true)
	path := filepath.Join("testdata", "card.svg")
	if *update {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) // #nosec G304 -- fixed test path
	if err != nil {
		t.Fatalf("%v (run with -update to write it)", err)
	}
	if got != string(want) {
		t.Errorf("card differs from %s; run with -update and look at it", path)
	}
}

// The card is well-formed XML whatever the project and task names hold, and
// says what its numbers are.
func TestCardContent(t *testing.T) {
	tests := []struct {
		name      string
		r         func() *Receipt
		project   string
		builtWith bool
		want      []string
		not       []string
	}{
		{
			name: "headline", r: cardReceipt, project: "acme", builtWith: true,
			want: []string{">2</text>", "tasks done", ">PRs, 1 passed CI<", "2h 5m", "$1.65", "50m of agent time · 1 blocked · 1 failed attempt", "claude · codex", "Built with orch", "finished Sep 15, 2026"},
		},
		{
			name: "no built-with", r: cardReceipt, project: "acme", builtWith: false,
			not: []string{"Built with orch"},
		},
		{
			name: "markup in the project name", r: cardReceipt, project: `<script>&"x"`, builtWith: true,
			want: []string{"&lt;script&gt;&amp;&#34;x&#34;"},
			not:  []string{"<script>"},
		},
		{
			name: "estimated only, no PRs, unfinished", project: "p",
			r: func() *Receipt {
				return &Receipt{RunID: "r2", Dispatches: 1, TotalCostUSD: 0.4, EstimatedCostUSD: 0.4}
			},
			want: []string{"~$0.40", ">agent run<", "still running", ">0</text>"},
		},
		{
			name: "no cost data", project: "p",
			r:    func() *Receipt { return &Receipt{RunID: "r3", Finished: true, FinishedAt: "2026-09-15T10:05:00Z"} },
			want: []string{">—</text>"},
		},
		{
			name: "long project name", project: strings.Repeat("x", 80), r: cardReceipt,
			want: []string{strings.Repeat("x", 33) + "…"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svg := tt.r().SVG(tt.project, tt.builtWith)
			if err := xml.Unmarshal([]byte(svg), new(struct{})); err != nil {
				t.Fatalf("not well-formed: %v\n%s", err, svg)
			}
			for _, w := range tt.want {
				if !strings.Contains(svg, w) {
					t.Errorf("missing %q", w)
				}
			}
			for _, n := range tt.not {
				if strings.Contains(svg, n) {
					t.Errorf("has %q", n)
				}
			}
		})
	}
}
