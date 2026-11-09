package snapshot

import (
	"github.com/Guria-Studio/orch/internal/evidence"
	"github.com/Guria-Studio/orch/internal/model"
)

// DeliverableEvidence is how a done deliverable was checked, as a client
// reads it (#520): the kinds of check it passed, whether another model
// reviewed it, its E2E walk-throughs in numbers, the steps of its @evidence
// test — written by the tests in the client's language, never translated —
// and, when the operator opted in (evidence.publish.video), its video and
// poster as paths inside the published site. No ids, models, commands or
// findings: the operator's evidence stays the operator's.
type DeliverableEvidence struct {
	Checked  []string  `json:"checked,omitempty"`
	Reviewed bool      `json:"reviewed"`
	E2E      *E2ECount `json:"e2e,omitempty"`
	Steps    []string  `json:"steps,omitempty"`
	Video    string    `json:"video,omitempty"`
	Poster   string    `json:"poster,omitempty"`
}

// E2ECount is how many of a deliverable's E2E walk-throughs passed.
type E2ECount struct {
	Passed int `json:"passed"`
	Total  int `json:"total"`
}

// checkOrder is the order a client reads the kinds of check in; install and
// unnamed commands are not checks to a client.
var checkOrder = []string{"tests", "typecheck", "lint", "build", "e2e"}

// deliverableEvidence is t's evidence for the client, nil when it is not
// done or has none that passed.
func deliverableEvidence(t model.Task, in Input) *DeliverableEvidence {
	m, ok := in.Evidence[t.ID]
	if !ok || t.Status != model.StatusDone || m.Verdict == evidence.VerdictFailed {
		return nil
	}
	e := &DeliverableEvidence{Reviewed: m.Verdict == evidence.VerdictPassed && m.Review != nil && m.Review.Reviewer != ""}
	kinds := map[string]bool{}
	for _, c := range m.Checks {
		kinds[c.Kind] = true
	}
	if m.E2E != nil {
		kinds["e2e"] = true
		e.E2E = &E2ECount{Passed: m.E2E.Passed, Total: m.E2E.Passed + m.E2E.Failed + m.E2E.Flaky}
	}
	for _, k := range checkOrder {
		if kinds[k] {
			e.Checked = append(e.Checked, k)
		}
	}
	media := evidence.ForClient(m, in.EvidenceMedia, in.MaxVideoBytes)
	if w := media.Walkthrough; w != nil {
		for _, s := range w.Steps {
			if s, ok := evidence.Safe(s, ""); ok {
				e.Steps = append(e.Steps, s)
			}
		}
	}
	e.Video, e.Poster = media.Video, media.Poster
	return e
}

// StripEvidenceMedia is snap without any evidence video or poster: what is
// published where the media do not go (`--to cloud`, #520 §9).
func StripEvidenceMedia(snap Snapshot) Snapshot {
	snap.Milestones = append([]Milestone(nil), snap.Milestones...)
	for i := range snap.Milestones {
		pkgs := append([]Package(nil), snap.Milestones[i].Packages...)
		for j := range pkgs {
			ds := append([]Deliverable(nil), pkgs[j].Deliverables...)
			for k := range ds {
				if e := ds[k].Evidence; e != nil {
					c := *e
					c.Video, c.Poster = "", ""
					ds[k].Evidence = &c
				}
			}
			pkgs[j].Deliverables = ds
		}
		snap.Milestones[i].Packages = pkgs
	}
	return snap
}

// MediaPaths are the site paths of every evidence video and poster snap
// shows: the files an export has to carry.
func MediaPaths(snap Snapshot) []string {
	var out []string
	for _, m := range snap.Milestones {
		for _, p := range m.Packages {
			for _, d := range p.Deliverables {
				if e := d.Evidence; e != nil {
					for _, s := range []string{e.Video, e.Poster} {
						if s != "" {
							out = append(out, s)
						}
					}
				}
			}
		}
	}
	return out
}
