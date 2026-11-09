package dashboard

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Guria-Studio/orch/internal/config"
	"github.com/Guria-Studio/orch/internal/tunnel"
)

// fakeFlow records the steps it was asked to take.
type fakeFlow struct {
	calls []string
	fail  error
}

func (f *fakeFlow) State() any { return map[string]any{"calls": f.calls} }
func (f *fakeFlow) Plan(idea string) error {
	f.calls = append(f.calls, "plan:"+idea)
	return f.fail
}
func (f *fakeFlow) Apply(context.Context) error { f.calls = append(f.calls, "apply"); return f.fail }
func (f *fakeFlow) Run() error                  { f.calls = append(f.calls, "run"); return f.fail }

func newFlowServer(t *testing.T, profile Profile, token string, flow ProjectFlow) *Server {
	t.Helper()
	root := writeProject(t, "spec_root: specs\n", testTasksJSON)
	s, err := New(cfg(profile, token), Options{
		Static: spaHandler(t),
		State:  &fakeState{},
		Paths:  config.Paths{Root: root, ID: "demo", ConfigYAML: filepath.Join(root, "config.yaml")},
		Flow:   flow,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// #548: plan, apply and run from the dashboard are operator writes, gated as
// steer is: a stakeholder, a caller off this machine or a page from another
// site is refused, and none of them reached the flow.
func TestFlowStepsRefuseAnyoneButTheLocalOperator(t *testing.T) {
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
			flow := &fakeFlow{}
			s := newFlowServer(t, tc.profile, token, flow)
			for _, path := range []string{"/api/flow/plan", "/api/flow/apply", "/api/flow/run"} {
				if rec := steerPost(t, s, path, `{"idea":"x"}`, tc.remote, tc.origin); rec.Code != http.StatusForbidden {
					t.Errorf("%s = %d, want 403: %s", path, rec.Code, rec.Body)
				}
			}
			if len(flow.calls) != 0 {
				t.Errorf("a refused request reached the flow: %v", flow.calls)
			}
		})
	}
}

func TestFlowStepsForTheLocalOperator(t *testing.T) {
	flow := &fakeFlow{}
	s := newFlowServer(t, ProfileOperator, "", flow)
	for _, path := range []string{"/api/flow/plan", "/api/flow/apply", "/api/flow/run"} {
		if rec := steerPost(t, s, path, `{"idea":"a todo app"}`, "127.0.0.1:50000", "http://127.0.0.1:7420"); rec.Code != http.StatusOK {
			t.Fatalf("%s = %d: %s", path, rec.Code, rec.Body)
		}
	}
	if got := strings.Join(flow.calls, ","); got != "plan:a todo app,apply,run" {
		t.Errorf("calls = %s", got)
	}
	if res := get(t, s, "/api/flow"); res.StatusCode != http.StatusOK {
		t.Errorf("GET /api/flow = %d", res.StatusCode)
	}
	flow.fail = errors.New("another orch run is already running")
	rec := steerPost(t, s, "/api/flow/run", "", "127.0.0.1:50000", "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "already running") {
		t.Errorf("a failed step = %d: %s", rec.Code, rec.Body)
	}
}

func TestFlowRoutesAreOffWithoutAFlow(t *testing.T) {
	s := newFlowServer(t, ProfileOperator, "", nil)
	if rec := steerPost(t, s, "/api/flow/run", "", "127.0.0.1:50000", ""); rec.Code != http.StatusNotFound {
		t.Errorf("run with no flow = %d, want 404", rec.Code)
	}
	if res := get(t, s, "/api/flow"); res.StatusCode != http.StatusNotFound {
		t.Errorf("GET /api/flow with no flow = %d, want 404", res.StatusCode)
	}
}

// #548: Share used to need tunnel.enabled in config.yaml and a restart. The
// local operator now turns it on from the page: config.yaml says so for the
// next start, and this dashboard can start a tunnel at once.
func TestShareTurnsTheTunnelOnWithoutARestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := newTunnelServer(t, ProfileOperator, "", TunnelOptions{Manager: tunnel.NewManager(t.TempDir(), nil, 10)})
	if rec := steerPost(t, s, "/api/tunnel/enable", "", "192.168.1.20:50000", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("enable from another machine = %d, want 403", rec.Code)
	}
	if rec := getWithHost(t, s, "/api/tunnel/status", "127.0.0.1:7420"); rec.Code != http.StatusNotFound {
		t.Fatalf("status before enable = %d, want 404", rec.Code)
	}
	if rec := steerPost(t, s, "/api/tunnel/enable", "", "127.0.0.1:50000", "http://127.0.0.1:7420"); rec.Code != http.StatusOK {
		t.Fatalf("enable = %d: %s", rec.Code, rec.Body)
	}
	if rec := getWithHost(t, s, "/api/tunnel/status", "127.0.0.1:7420"); rec.Code != http.StatusOK {
		t.Errorf("status after enable = %d, want 200: %s", rec.Code, rec.Body)
	}
	var caps capabilitiesPayload
	decodeBody(t, getWithHost(t, s, "/api/tunnel/capabilities", "127.0.0.1:7420"), &caps)
	if !caps.Enabled || caps.Reason == tunnel.ReasonConfigDisabled {
		t.Errorf("capabilities after enable: %+v", caps)
	}
	b, err := os.ReadFile(s.paths.ConfigYAML)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(s.paths.ConfigYAML, s.paths.Root)
	if err != nil || !loaded.Config.Tunnel.Enabled {
		t.Errorf("config.yaml does not keep tunnel.enabled (err %v):\n%s", err, b)
	}
}
