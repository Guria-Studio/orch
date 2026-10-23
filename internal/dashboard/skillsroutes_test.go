package dashboard

import (
	"net/http"
	"testing"

	"github.com/hectorcanaimero/orch/internal/skills"
)

// Each skill says, per agent CLI, whether it is installed as this orch
// would install it — and asking writes nothing.
func TestSkillsReportsTheInstallStatePerTarget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orch, ok := skills.Get("orch")
	if !ok {
		t.Fatal("no embedded orch skill")
	}
	if _, err := skills.Install(orch, skills.TargetClaude, skills.Options{}); err != nil {
		t.Fatal(err)
	}

	s := newReadServer(t, &fakeState{})
	var got skillsPayload
	decode(t, get(t, s, "/api/skills"), &got)
	if len(got.Skills) < 2 {
		t.Fatalf("skills = %+v, want every embedded one", got.Skills)
	}
	state := map[string]map[string]string{}
	for _, row := range got.Skills {
		state[row.Name] = map[string]string{}
		for _, ts := range row.Targets {
			state[row.Name][ts.Target] = ts.State
		}
	}
	if state["orch"]["claude"] != "current" || state["orch"]["codex"] != "missing" || state["orch-plan"]["claude"] != "missing" {
		t.Errorf("states = %v", state)
	}
	// A second read is the same: the dry run installed nothing.
	var again skillsPayload
	decode(t, get(t, s, "/api/skills"), &again)
	if again.Skills[1].Targets[0].State != got.Skills[1].Targets[0].State {
		t.Error("reading the skills changed what is installed")
	}
	if resp := get(t, newGatedServer(t, &fakeState{}), "/api/skills?token=test-token-stakeholder"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("stakeholder: %d, want 403", resp.StatusCode)
	}
}
