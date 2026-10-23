package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const setFixture = `# orch project config
spec_root: specs   # where atomize reads

tunnel:
  # share the dashboard
  enabled: false
`

func writeSetFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(setFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// Each key lands typed as the Config field it names, nested blocks are
// created on demand, and what the file already says — comments included —
// stays.
func TestSetWritesTypedValuesAndKeepsComments(t *testing.T) {
	path := writeSetFixture(t)
	err := Set(path, "", map[string]string{
		"tunnel.enabled":                  "true",
		"concurrency.global_max":          "3",
		"notifications.budget_alert_pct":  "75.5",
		"verify.commands":                 "make lint, make test",
		"verify.app.url":                  "http://localhost:3000",
		"concurrency.per_provider.claude": "2",
		"dashboard.language":              "es",
	})
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	res, err := Load(path, "")
	if err != nil {
		t.Fatalf("Load after Set: %v", err)
	}
	c := res.Config
	if !c.Tunnel.Enabled || c.Concurrency.GlobalMax != 3 || c.Notifications.BudgetAlertPct != 75.5 ||
		c.Verify.App.URL != "http://localhost:3000" || c.Concurrency.PerProvider["claude"] != 2 {
		t.Errorf("config after Set = %+v", c)
	}
	if got := strings.Join(c.Verify.Commands, "|"); got != "make lint|make test" {
		t.Errorf("verify.commands = %q", got)
	}
	body, _ := os.ReadFile(path) // #nosec G304 -- path is under t.TempDir()
	for _, keep := range []string{"# orch project config", "# where atomize reads", "# share the dashboard"} {
		if !strings.Contains(string(body), keep) {
			t.Errorf("lost %q:\n%s", keep, body)
		}
	}
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v", res.Warnings)
	}
}

func TestSetRefusesWhatCannotBeWritten(t *testing.T) {
	for _, tc := range []struct {
		key, value, want string
	}{
		{"tunel.enabled", "true", "not a config key"},
		{"tunnel.provider", "bore", "not a config key"},
		{"tunnel.enabled", "maybe", "true or false"},
		{"concurrency.global_max", "three", "a whole number"},
		{"dashboard.profile", "boss", "dashboard.profile"},
		{"tunnel", "true", "not a config key"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			path := writeSetFixture(t)
			err := Set(path, "", map[string]string{tc.key: tc.value})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to say %q", err, tc.want)
			}
			// All or nothing: a refused set leaves the file as it was.
			if body, _ := os.ReadFile(path); string(body) != setFixture { // #nosec G304 -- path is under t.TempDir()
				t.Errorf("file changed:\n%s", body)
			}
		})
	}
}

func TestUnsetRemovesTheKey(t *testing.T) {
	path := writeSetFixture(t)
	if err := Unset(path, "", []string{"tunnel.enabled"}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(path) // #nosec G304 -- path is under t.TempDir()
	if strings.Contains(string(body), "enabled") {
		t.Errorf("tunnel.enabled still there:\n%s", body)
	}
	if err := Unset(path, "", []string{"tunnel.enabled"}); err != nil {
		t.Errorf("unsetting an absent key: %v", err)
	}
}

func TestSetCreatesAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Set(path, "", map[string]string{"verify.enabled": "true"}); err != nil {
		t.Fatal(err)
	}
	res, err := Load(path, "")
	if err != nil || !res.Config.Verify.Enabled {
		t.Fatalf("Load = %+v, %v", res.Config.Verify, err)
	}
}

// Every key the loader reads can be set: knownKeys and the Config struct's
// yaml tags agree.
func TestEveryKnownKeyHasAType(t *testing.T) {
	for key := range knownKeys {
		if strings.HasSuffix(key, ".*") {
			continue
		}
		if _, err := keyType(key); err != nil {
			t.Errorf("%s: %v", key, err)
		}
	}
}
