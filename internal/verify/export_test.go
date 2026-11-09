package verify

import (
	"github.com/Guria-Studio/orch/internal/knowledge"
	"github.com/Guria-Studio/orch/internal/model"
)

// What verify_test.go, an external test package since engine.FromProject
// builds a Verifier (engine imports verify, so a test of verify that drives
// engine.OneShot cannot live inside it), reaches of the unexported.
var (
	ReadyPoll = &readyPoll
	Checklist = checklist
	WithRules = withRules
	Run       = run
)

// Reviewer is v.reviewer.
func Reviewer(v Verifier, writer model.Backend) model.Backend { return v.reviewer(writer) }

// Rules is v.rules.
func Rules(v Verifier, taskID, dir string, changed []string) []knowledge.Lesson {
	return v.rules(taskID, dir, changed)
}
