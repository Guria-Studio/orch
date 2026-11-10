package project

import (
	"path"
	"sort"
	"strings"
	"time"

	"github.com/Guria-Studio/orch/internal/state"
)

// The radar effectiveness report (#522): how many of the collisions a
// project ran into the conflict radar (#482) saw coming, and how many of its
// warnings came true. Read-only, from the event log.

// Engine reasons the report reads collisions from. Engine strings, repeated
// here because project cannot import engine; engine's tests check that its
// reasons still start with these.
const (
	// RadarPRConflictReason starts a ci_blocked reason when a PR conflicted
	// with its base and no retry was left (or retry.conflict is off).
	RadarPRConflictReason = "the PR conflicts with its base branch"
	// RadarLandConflictReason starts a fail reason when a task's local land
	// (vcs.provider: local) conflicted and no retry was left.
	RadarLandConflictReason = "the work conflicts with "
)

// Collision kinds, as RadarCollision.Kind.
const (
	RadarPRConflict        = "pr_conflict"         // ci_conflict_retry: the PR conflicted, sent to the agent
	RadarPRConflictBlocked = "pr_conflict_blocked" // ci_blocked: the PR conflicted, task blocked
	RadarLandConflict      = "land_conflict"       // a local land conflicted (retry or block)
	RadarBaseBroke         = "base_broke_verify"   // the base merged cleanly but broke the local verify
	RadarSemantic          = "semantic_collision"  // a block rule stopped the merge (#522)
)

// RadarReport is `orch radar report`.
type RadarReport struct {
	// From is the first instant counted, RFC 3339; "" for all of history.
	From        string           `json:"from"`
	Predictions RadarPredictions `json:"predictions"`
	Collisions  RadarCollisions  `json:"collisions"`
	// Recall is the share of collisions some earlier warning predicted;
	// null with no collision.
	Recall *float64 `json:"recall"`
	// Precision is the share of warned pairs that later collided; null
	// with no warning.
	Precision *float64 `json:"precision"`
	// Pairs are the warned pairs of tasks, oldest warning first.
	Pairs []RadarPair `json:"pairs"`
	// Events are the collisions, oldest first.
	Events []RadarCollision `json:"events"`
}

// RadarPredictions counts the radar's warnings.
type RadarPredictions struct {
	FilesOverlap   int `json:"files_overlap"`
	BranchConflict int `json:"branch_conflict"`
	// Pairs is the distinct pairs of tasks warned about.
	Pairs int `json:"pairs"`
	// PairsCollided is how many of them collided after the warning.
	PairsCollided int `json:"pairs_collided"`
}

// RadarCollisions counts what actually collided.
type RadarCollisions struct {
	Total     int            `json:"total"`
	Predicted int            `json:"predicted"`
	Escaped   int            `json:"escaped"`
	ByKind    map[string]int `json:"by_kind"`
}

// RadarPair is one pair of tasks the radar warned about.
type RadarPair struct {
	Tasks    [2]string `json:"tasks"`
	Kinds    []string  `json:"kinds"`
	Files    []string  `json:"files"`
	FirstAt  string    `json:"first_at"`
	Collided bool      `json:"collided"`
}

// RadarCollision is one collision and whether it was predicted.
type RadarCollision struct {
	TaskID    string   `json:"task_id"`
	Kind      string   `json:"kind"`
	EventType string   `json:"event_type"`
	At        string   `json:"at"`
	Files     []string `json:"files"`
	// With are the tasks it collided with, when the event says
	// (ci_conflict_retry's caused_by, a semantic collision's other branch).
	With      []string `json:"with"`
	Predicted bool     `json:"predicted"`
	// PredictedBy are the warned pairs that matched it, "A ~ B".
	PredictedBy []string `json:"predicted_by"`
	// Match is how the best one matched: "with" (the pair is the one it
	// collided with), "files" (a file in common) or "task" (the collision
	// names no file nor task, so any earlier warning on it counts); "" when
	// none did.
	Match string `json:"match"`
}

// radarMatchRank orders the ways a warning can match a collision.
var radarMatchRank = map[string]int{"with": 3, "files": 2, "task": 1}

// radarWarning is a files_overlap or branch_conflict event.
type radarWarning struct {
	task, other string
	kind        string
	files       []string
}

// RadarEffectiveness reads the radar's warnings and the collisions in
// events (chronological, as AllEvents returns them) from `from` on (zero:
// all), and matches each collision with the warnings before it.
//
// A collision on task T is predicted when an earlier warning names T (on
// either side) and either the other task is the one T collided with, or the
// warning and the collision share a file (a warned directory or glob covers
// the files under it). A collision that names neither files nor a task (a
// clean merge that broke the verify, an old conflict block) counts any
// earlier warning on T. A warned pair collided when a collision on either of
// its tasks matched it.
func RadarEffectiveness(events []state.Event, from time.Time) RadarReport {
	rep := RadarReport{Pairs: []RadarPair{}, Events: []RadarCollision{}}
	if !from.IsZero() {
		rep.From = from.UTC().Format(time.RFC3339)
	}
	rep.Collisions.ByKind = map[string]int{}
	var warnings []radarWarning
	pairIdx := map[[2]string]int{}
	for _, e := range events {
		if !from.IsZero() {
			if ts, ok := state.ParseTS(e.TS); !ok || ts.Before(from) {
				continue
			}
		}
		if w, ok := asWarning(e); ok {
			switch w.kind {
			case "files_overlap":
				rep.Predictions.FilesOverlap++
			default:
				rep.Predictions.BranchConflict++
			}
			warnings = append(warnings, w)
			key := pairKey(w.task, w.other)
			j, seen := pairIdx[key]
			if !seen {
				j = len(rep.Pairs)
				pairIdx[key] = j
				rep.Pairs = append(rep.Pairs, RadarPair{Tasks: key, Kinds: []string{}, Files: []string{}, FirstAt: e.TS})
			}
			p := &rep.Pairs[j]
			p.Kinds = addUnique(p.Kinds, w.kind)
			for _, f := range w.files {
				p.Files = addUnique(p.Files, f)
			}
			continue
		}
		c, ok := asCollision(e)
		if !ok {
			continue
		}
		for _, w := range warnings {
			m := matchWarning(w, c)
			if m == "" {
				continue
			}
			key := pairKey(w.task, w.other)
			c.PredictedBy = addUnique(c.PredictedBy, key[0]+" ~ "+key[1])
			rep.Pairs[pairIdx[key]].Collided = true
			if radarMatchRank[m] > radarMatchRank[c.Match] {
				c.Match = m
			}
		}
		c.Predicted = len(c.PredictedBy) > 0
		rep.Collisions.Total++
		rep.Collisions.ByKind[c.Kind]++
		if c.Predicted {
			rep.Collisions.Predicted++
		} else {
			rep.Collisions.Escaped++
		}
		rep.Events = append(rep.Events, c)
	}
	rep.Predictions.Pairs = len(rep.Pairs)
	for _, p := range rep.Pairs {
		if p.Collided {
			rep.Predictions.PairsCollided++
		}
	}
	if n := rep.Collisions.Total; n > 0 {
		r := float64(rep.Collisions.Predicted) / float64(n)
		rep.Recall = &r
	}
	if n := rep.Predictions.Pairs; n > 0 {
		p := float64(rep.Predictions.PairsCollided) / float64(n)
		rep.Precision = &p
	}
	return rep
}

func asWarning(e state.Event) (radarWarning, bool) {
	if e.EventType != "files_overlap" && e.EventType != "branch_conflict" {
		return radarWarning{}, false
	}
	other, _ := e.Extra["with"].(string)
	if e.TaskID == "" || other == "" || other == e.TaskID {
		return radarWarning{}, false
	}
	return radarWarning{task: e.TaskID, other: other, kind: e.EventType,
		files: extraStrings(e.Extra, "files")}, true
}

// asCollision reads a collision off an event, or false.
func asCollision(e state.Event) (RadarCollision, bool) {
	c := RadarCollision{TaskID: e.TaskID, EventType: e.EventType, At: e.TS,
		Files: []string{}, With: []string{}, PredictedBy: []string{}}
	reason, _ := e.Extra["reason"].(string)
	switch e.EventType {
	case "ci_conflict_retry":
		if agent, _ := e.Extra["agent"].(bool); !agent {
			// A clean merge orch pushed itself: nothing collided.
			return c, false
		}
		c.Kind = RadarPRConflict
		if local, _ := e.Extra["local"].(bool); local {
			c.Kind = RadarLandConflict
		}
		if vf, _ := e.Extra["verify_failed"].(bool); vf {
			c.Kind = RadarBaseBroke
		}
		c.Files = nonNil(extraStrings(e.Extra, "files"))
		c.With = nonNil(extraStrings(e.Extra, "caused_by"))
	case "ci_blocked":
		if !strings.HasPrefix(reason, RadarPRConflictReason) {
			return c, false
		}
		c.Kind = RadarPRConflictBlocked
		c.Files = filesIn(reason, "(conflicts in ", ")")
	case "fail":
		if !strings.HasPrefix(reason, RadarLandConflictReason) {
			return c, false
		}
		c.Kind = RadarLandConflict
		if _, rest, ok := strings.Cut(reason, " in "); ok {
			c.Files = filesIn(" in "+rest, " in ", " (kept on ")
			if len(c.Files) == 0 {
				c.Files = filesIn(" in "+rest, " in ", ", and merging ")
			}
		}
	case "semantic_collision":
		if blocked, _ := e.Extra["blocked"].(bool); !blocked {
			return c, false
		}
		c.Kind = RadarSemantic
		if f, _ := e.Extra["file"].(string); f != "" {
			c.Files = []string{f}
		}
		if w, _ := e.Extra["with"].(string); w != "" && w != "base" {
			c.With = []string{w}
		}
	default:
		return c, false
	}
	return c, c.TaskID != ""
}

// matchWarning is how warning w predicts collision c, or "".
func matchWarning(w radarWarning, c RadarCollision) string {
	var other string
	switch c.TaskID {
	case w.task:
		other = w.other
	case w.other:
		other = w.task
	default:
		return ""
	}
	for _, t := range c.With {
		if t == other {
			return "with"
		}
	}
	for _, wf := range w.files {
		for _, cf := range c.Files {
			if fileCovers(wf, cf) || fileCovers(cf, wf) {
				return "files"
			}
		}
	}
	if len(c.Files) == 0 && len(c.With) == 0 {
		return "task"
	}
	return ""
}

// fileCovers reports whether entry (a path, a directory ending in "/", or a
// glob, as `files` allows) names file.
func fileCovers(entry, file string) bool {
	switch {
	case entry == file:
		return true
	case strings.HasSuffix(entry, "/"):
		return strings.HasPrefix(file, entry)
	case strings.ContainsAny(entry, "*?["):
		if ok, err := path.Match(entry, file); err == nil && ok {
			return true
		}
		// `**` and a glob over a directory: the literal directory before
		// the first wildcard, as the radar itself compares them.
		i := strings.IndexAny(entry, "*?[")
		return strings.HasPrefix(file, entry[:strings.LastIndex(entry[:i], "/")+1])
	}
	return false
}

// filesIn is the comma-separated list between open and close in s.
func filesIn(s, open, closing string) []string {
	_, rest, ok := strings.Cut(s, open)
	if !ok {
		return []string{}
	}
	list, _, ok := strings.Cut(rest, closing)
	if !ok {
		return []string{}
	}
	out := []string{}
	for _, f := range strings.Split(list, ", ") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func extraStrings(extra map[string]any, key string) []string {
	raw, _ := extra[key].([]any)
	var out []string
	for _, v := range raw {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	if ss, ok := extra[key].([]string); ok {
		out = append(out, ss...)
	}
	return out
}

func pairKey(a, b string) [2]string {
	k := []string{a, b}
	sort.Strings(k)
	return [2]string{k[0], k[1]}
}

func addUnique(list []string, s string) []string {
	for _, v := range list {
		if v == s {
			return list
		}
	}
	return append(list, s)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
