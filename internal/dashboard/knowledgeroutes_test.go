package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hectorcanaimero/orch/internal/knowledge"
	"github.com/hectorcanaimero/orch/internal/model"
	"github.com/hectorcanaimero/orch/internal/state"
)

// newKnowledgeServer is the steer fixture with the Knowledge page on and two
// lessons: one T-1's prompt carried, T-1 done on its first try.
func newKnowledgeServer(t *testing.T, profile Profile, token string) (*Server, state.Backend, []knowledge.Lesson) {
	t.Helper()
	ctx := context.Background()
	s, backend := newSteerServer(t, profile, token)
	s.knowledge = &knowledge.Editor{Root: s.paths.Root, Events: backend, Source: "cli"}
	var lessons []knowledge.Lesson
	for _, l := range []knowledge.Lesson{
		{Title: "Run the migrations before the tests", Body: "pytest needs the schema.", Area: []string{"db/"}, Task: "T-1"},
		{Title: "Lint with ruff", Body: "ruff check, not flake8.", Source: "review"},
	} {
		added, err := knowledge.Add(s.paths.Root, l, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		lessons = append(lessons, added)
	}
	if err := backend.AppendEvent(ctx, "run-1", state.Event{EventType: knowledge.EventUsed, TaskID: "T-1",
		TS: time.Now().UTC().Format(time.RFC3339), Extra: map[string]any{"lessons": []string{lessons[0].ID}}}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Transition(ctx, "T-1", model.StatusDone, state.Note{}); err != nil {
		t.Fatal(err)
	}
	return s, backend, lessons
}

func decodeKnowledge(t *testing.T, body string) knowledgePayload {
	t.Helper()
	var out knowledgePayload
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return out
}

// The list carries each lesson's use; ?q= ranks them through
// knowledge.Search, the function orch_knowledge_search answers with.
func TestKnowledgeListsLessonsWithTheirUseAndSearches(t *testing.T) {
	s, _, lessons := newKnowledgeServer(t, ProfileOperator, "")
	rec := operatorSend(t, s, http.MethodGet, "/api/knowledge", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", rec.Code, rec.Body)
	}
	got := decodeKnowledge(t, rec.Body.String())
	if len(got.Lessons) != 2 || len(got.Sources) != len(knowledge.Sources) {
		t.Fatalf("payload %+v", got)
	}
	for _, l := range got.Lessons {
		want := knowledge.Use{}
		if l.ID == lessons[0].ID {
			want = knowledge.Use{Prompts: 1, Tasks: 1, FirstTry: 1}
		}
		if l.Use != want {
			t.Errorf("%s use = %+v, want %+v", l.ID, l.Use, want)
		}
	}

	rec = operatorSend(t, s, http.MethodGet, "/api/knowledge?q=ruff+flake8", "")
	got = decodeKnowledge(t, rec.Body.String())
	if rec.Code != http.StatusOK || len(got.Lessons) != 1 || got.Lessons[0].ID != lessons[1].ID {
		t.Errorf("search = %d %+v; want only the ruff lesson", rec.Code, got.Lessons)
	}
}

// Add, edit and delete go through knowledge.Editor: the files change and each
// change is an event naming the dashboard.
func TestKnowledgeAddEditDelete(t *testing.T) {
	s, backend, lessons := newKnowledgeServer(t, ProfileOperator, "")

	rec := operatorSend(t, s, http.MethodPost, "/api/knowledge", `{"title":"Use the shared network","body":"docker network create shared","area":["compose/"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d: %s", rec.Code, rec.Body)
	}
	var added knowledge.Lesson
	_ = json.Unmarshal(rec.Body.Bytes(), &added)
	if added.Source != "operator" || added.ID == "" {
		t.Errorf("added %+v", added)
	}
	if rec := operatorSend(t, s, http.MethodPost, "/api/knowledge", `{"title":"no body"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("POST with no body = %d, want 400", rec.Code)
	}

	rec = operatorSend(t, s, http.MethodPatch, "/api/knowledge/"+lessons[1].ID, `{"title":"Lint with ruff only","body":"ruff.","source":"review"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH = %d: %s", rec.Code, rec.Body)
	}
	if rec := operatorSend(t, s, http.MethodPatch, "/api/knowledge/nope", `{"title":"x","body":"y"}`); rec.Code != http.StatusNotFound {
		t.Errorf("PATCH of an unknown lesson = %d, want 404", rec.Code)
	}

	if rec := operatorSend(t, s, http.MethodDelete, "/api/knowledge/"+lessons[0].ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE = %d: %s", rec.Code, rec.Body)
	}

	now, _ := knowledge.Load(s.paths.Root)
	titles := map[string]string{}
	for _, l := range now {
		titles[l.ID] = l.Title
	}
	if len(now) != 2 || titles[lessons[1].ID] != "Lint with ruff only" || titles[added.ID] == "" {
		t.Errorf("lessons after the edits: %v", titles)
	}

	events, err := backend.(*state.SQLite).AllEvents(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, e := range events {
		if e.EventType == knowledge.EventEdited && e.Extra["source"] == "dashboard" {
			actions = append(actions, e.Extra["action"].(string))
		}
	}
	if strings.Join(actions, ",") != "added,edited,removed" {
		t.Errorf("knowledge_edited events %v, want added,edited,removed", actions)
	}
}

// The stakeholder profile reaches neither the list nor a write; nor does
// anyone but the local operator write.
func TestKnowledgeRefusesTheStakeholderAndRemoteWrites(t *testing.T) {
	for _, tc := range []struct {
		name, remote, origin string
		profile              Profile
	}{
		{"stakeholder", "127.0.0.1:50000", "", ProfileStakeholder},
		{"not loopback", "192.168.1.20:50000", "", ProfileOperator},
		{"cross-origin", "127.0.0.1:50000", "https://evil.example", ProfileOperator},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := ""
			if tc.profile == ProfileStakeholder {
				token = "test-token-stakeholder"
			}
			s, _, lessons := newKnowledgeServer(t, tc.profile, token)
			calls := []struct{ method, path, body string }{
				{http.MethodPost, "/api/knowledge", `{"title":"x","body":"y"}`},
				{http.MethodPatch, "/api/knowledge/" + lessons[0].ID, `{"title":"x","body":"y"}`},
				{http.MethodDelete, "/api/knowledge/" + lessons[0].ID, ""},
			}
			if tc.profile == ProfileStakeholder {
				calls = append(calls, struct{ method, path, body string }{http.MethodGet, "/api/knowledge", ""})
			}
			for _, c := range calls {
				req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
				req.Host = "127.0.0.1:7420"
				req.RemoteAddr = tc.remote
				if tc.origin != "" {
					req.Header.Set("Origin", tc.origin)
				}
				req.Header.Set("Authorization", "Bearer test-token-stakeholder")
				rec := httptest.NewRecorder()
				s.Handler().ServeHTTP(rec, req)
				if rec.Code != http.StatusForbidden {
					t.Errorf("%s %s = %d, want 403: %s", c.method, c.path, rec.Code, rec.Body)
				}
			}
			if now, _ := knowledge.Load(s.paths.Root); len(now) != 2 || now[0].Title != lessons[0].Title && now[1].Title != lessons[0].Title {
				t.Errorf("a refused request changed the lessons: %+v", now)
			}
		})
	}
}
