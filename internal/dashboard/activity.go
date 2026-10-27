package dashboard

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/hectorcanaimero/orch/internal/providers"
)

// /api/task/{task_id}/activity is what a running agent is doing: its
// dispatch log (`<state>/logs/<id>.log`, engine.LogPathFor), read from a byte
// offset and turned into steps — the file it opened, the command it ran, what
// it said. The page polls it with the offset it was last handed, so each
// request reads only what the agent wrote since.
//
// Operator-only. The log is the agent's whole transcript, tool output
// included, which can carry anything a command printed; no default
// stakeholder route names it, and none should.

const (
	// activityTail is how far back a first read starts: enough for the last
	// few dozen steps without shipping a long run's whole log.
	activityTail = 64 << 10
	// activityChunk caps one read. A line longer than this (a tool result
	// holding a large file) is skipped: the next read starts mid-line, and
	// the partial line is not JSON, so it yields no step.
	activityChunk = 1 << 20
)

// Activity is one read of a dispatch log: the endpoint's payload, and what
// `orch tui` tails for the selected task, so both show the same steps.
type Activity struct {
	// Offset is where the next read starts; pass it back as ?offset=.
	Offset int64            `json:"offset"`
	Steps  []providers.Step `json:"steps"`
	// Exists is false until the task's first dispatch writes its log.
	Exists bool `json:"exists"`
}

func (s *Server) activityRoutes() []route {
	return []route{
		{pattern: "GET /api/task/{task_id}/activity", name: "api_task_activity", handler: s.handleActivity},
	}
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("task_id")
	// The id becomes a file name: nothing that could leave the logs directory.
	if taskID == "" || taskID != filepath.Base(taskID) || taskID == "." || taskID == ".." {
		writeJSON(w, http.StatusBadRequest, errorPayload{Detail: "invalid task id: " + taskID})
		return
	}
	offset := int64(-1)
	if raw := r.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, errorPayload{Detail: "offset must be a byte count"})
			return
		}
		offset = n
	}

	payload, err := ReadActivity(filepath.Join(s.paths.StateDir(), "logs", taskID+".log"), offset)
	if err != nil {
		s.failRead(w, "activity", err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

// ReadActivity reads the steps in path from offset (-1: the tail) to the last
// complete line. An offset past the end means the log was replaced, so the
// read starts over.
func ReadActivity(path string, offset int64) (Activity, error) {
	out := Activity{Steps: []providers.Step{}}
	f, err := os.Open(path) // #nosec G304 -- the state dir plus a task id that cannot leave it
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("opening the dispatch log: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return out, fmt.Errorf("reading the dispatch log's size: %w", err)
	}
	out.Exists = true
	size := info.Size()
	switch {
	case offset < 0:
		offset = max(0, size-activityTail)
	case offset > size:
		offset = 0
	}
	buf := make([]byte, min(size-offset, activityChunk))
	n, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return out, fmt.Errorf("reading the dispatch log at %d: %w", offset, err)
	}
	buf = buf[:n]
	end := bytes.LastIndexByte(buf, '\n') + 1
	if end == 0 && n == activityChunk {
		end = n // one line longer than a chunk: skip it
	}
	for _, line := range bytes.Split(buf[:end], []byte("\n")) {
		out.Steps = append(out.Steps, providers.ActivityOf(line)...)
	}
	out.Offset = offset + int64(end)
	return out, nil
}
