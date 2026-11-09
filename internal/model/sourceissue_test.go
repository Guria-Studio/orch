package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// sourceIssue (#525) is optional: a task that has one keeps it through a
// decode/encode round trip, and a task that has none writes no key, so every
// tasks.json written before it stays byte for byte what it was.
func TestSourceIssueRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"kept, after specRef",
			`{"id":"F0.12.T1","phase":0,"title":"t","model":"m","specRef":"issue-12.md#F0.12.T1","sourceIssue":"https://github.com/acme/shop/issues/12"}`,
			`{"id":"F0.12.T1","phase":0,"title":"t","model":"m","specRef":"issue-12.md#F0.12.T1","sourceIssue":"https://github.com/acme/shop/issues/12"}`,
		},
		{
			"absent stays absent",
			`{"id":"A","phase":0,"title":"t","model":"m"}`,
			`{"id":"A","phase":0,"title":"t","model":"m"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var task Task
			if err := json.Unmarshal([]byte(tt.in), &task); err != nil {
				t.Fatal(err)
			}
			if _, leaked := task.Extra["sourceIssue"]; leaked {
				t.Error("sourceIssue decoded into Extra, want the typed field")
			}
			out, err := json.Marshal(task)
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tt.want {
				t.Errorf("got  %s\nwant %s", out, tt.want)
			}
		})
	}
}

func TestSourceIssueBuiltInGo(t *testing.T) {
	with, _ := json.Marshal(Task{ID: "A", Title: "t", Model: "m", SourceIssue: "https://github.com/o/r/issues/1"})
	if !strings.Contains(string(with), `"specRef":"","sourceIssue":"https://github.com/o/r/issues/1","comments":[]`) {
		t.Errorf("got %s, want sourceIssue between specRef and comments", with)
	}
	without, _ := json.Marshal(Task{ID: "A", Title: "t", Model: "m"})
	if strings.Contains(string(without), "sourceIssue") {
		t.Errorf("got %s, want no sourceIssue key on a task without one", without)
	}
}
