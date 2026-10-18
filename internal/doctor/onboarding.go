package doctor

import "fmt"

// Onboarding is how far a project got from `orch init` to an agent finishing
// a task (#378): read from the project's own state, telemetry on or off.
type Onboarding struct {
	// Template is what `orch init` scaffolded ("blank" for none); empty for
	// a project initialized before orch recorded it.
	Template string
	// DispatchBackend is the CLI the first task went to; empty before any.
	DispatchBackend string
	Dispatched      bool
	FinishedTask    bool
}

// CheckOnboarding shows the three steps and, until the last, the next thing
// to do. Never a warning: an unfinished onboarding is a project in progress,
// not a problem.
func CheckOnboarding(o Onboarding) Check {
	const name = "onboarding.progress"
	mark := func(done bool) string {
		if done {
			return "✓"
		}
		return "·"
	}
	first := "init ✓"
	if o.Template != "" {
		first = fmt.Sprintf("init ✓ (%s)", o.Template)
	}
	dispatch := "first dispatch " + mark(o.Dispatched)
	if o.Dispatched && o.DispatchBackend != "" {
		dispatch += " (" + o.DispatchBackend + ")"
	}
	detail := first + " → " + dispatch + " → first finished task " + mark(o.FinishedTask)
	switch {
	case !o.Dispatched:
		detail += " — next: `orch run` to send the first task to an agent"
	case !o.FinishedTask:
		detail += " — next: let `orch run` finish a task (`orch status` shows where it stands)"
	}
	return Check{Name: name, Status: StatusOK, Detail: detail}
}
