package cli

import (
	"testing"

	"github.com/Guria-Studio/orch/internal/knowledge"
	"github.com/Guria-Studio/orch/internal/state"
)

// orch knowledge stats splits the done tasks at the first lesson: a task done
// on its first dispatch with no CI retry is a first-try success, and every
// further dispatch or CI retry counts as a retry. Both timestamp spellings
// orch.db holds are read.
func TestKnowledgeStatsSplitsAtTheFirstLesson(t *testing.T) {
	lessons := []knowledge.Lesson{{Source: "agent", Created: "2026-10-20"}, {Source: "verify", Created: "2026-10-22"}}
	done := []state.TaskRuntime{
		{FinishedAt: "2026-10-19T10:00:00+00:00", Attempts: 1},
		{FinishedAt: "2026-10-19T11:00:00Z", Attempts: 3, CIAttempts: 1},
		{FinishedAt: "2026-10-21T09:00:00Z", Attempts: 1},
		{FinishedAt: "2026-10-23T09:00:00+00:00", Attempts: 2},
	}
	s := knowledgeStats(lessons, done)
	if s.Lessons != 2 || s.BySource["agent"] != 1 || s.BySource["verify"] != 1 || s.FirstLesson != "2026-10-20" {
		t.Errorf("lessons = %+v", s)
	}
	if want := (taskOutcomes{Tasks: 2, FirstTry: 1, FirstTryRate: 0.5, RetriesPerTsk: 1.5}); s.Before != want {
		t.Errorf("before = %+v, want %+v", s.Before, want)
	}
	if want := (taskOutcomes{Tasks: 2, FirstTry: 1, FirstTryRate: 0.5, RetriesPerTsk: 0.5}); s.Since != want {
		t.Errorf("since = %+v, want %+v", s.Since, want)
	}
}
