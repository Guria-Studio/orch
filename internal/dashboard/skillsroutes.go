package dashboard

import (
	"net/http"

	"github.com/Guria-Studio/orch/internal/skills"
)

func (s *Server) skillsRoutes() []route {
	return []route{
		{pattern: "GET /api/skills", name: "api_skills", handler: s.handleSkills},
	}
}

// skillTargets are where `orch install-skills --target` can put a skill, in
// the order the page shows them.
var skillTargets = []skills.Target{skills.TargetClaude, skills.TargetCodex, skills.TargetOpencode, skills.TargetCursor}

// skillState is one skill in one target: "current", "missing", or
// "different" (installed, but not the content this orch embeds).
type skillState struct {
	Target string `json:"target"`
	State  string `json:"state"`
	Path   string `json:"path"`
}

type skillRow struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Targets     []skillState `json:"targets"`
}

type skillsPayload struct {
	Skills []skillRow `json:"skills"`
}

// handleSkills lists orch's embedded skills and, per agent CLI, whether each
// is installed as this orch would install it — `orch install-skills
// --dry-run`'s answer, computed the same way (skills.Install with DryRun),
// so it writes nothing. Installing stays the CLI's: the page shows the
// command. Operator-only.
func (s *Server) handleSkills(w http.ResponseWriter, _ *http.Request) {
	all, err := skills.List()
	if err != nil {
		s.failRead(w, "skills", err)
		return
	}
	out := skillsPayload{Skills: make([]skillRow, 0, len(all))}
	for _, sk := range all {
		row := skillRow{Name: sk.Name, Description: sk.Description()}
		for _, target := range skillTargets {
			res, err := skills.Install(sk, target, skills.Options{ProjectRoot: s.paths.Root, DryRun: true})
			if err != nil {
				s.failRead(w, "skills", err)
				return
			}
			state := "different"
			switch res.Action {
			case skills.ActionUnchanged:
				state = "current"
			case skills.ActionInstalled:
				state = "missing"
			}
			row.Targets = append(row.Targets, skillState{Target: string(target), State: state, Path: res.Path})
		}
		out.Skills = append(out.Skills, row)
	}
	writeJSON(w, http.StatusOK, out)
}
