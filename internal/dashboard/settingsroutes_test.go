package dashboard

import (
	"net/http"
	"testing"
)

// The effective config by section, every leaf under the dotted key
// docs/CONFIG.md uses, defaults included.
func TestEffectiveConfigListsEveryKeyBySection(t *testing.T) {
	s, _ := newMoneyServer(t, &fakeState{}, map[string]string{"config.yaml": "spec_root: specs\ntunnel:\n  enabled: true\nnotifications:\n  slack_webhook: https://hooks.slack.test/T0/B0/secret\n"})
	var got effectiveConfigPayload
	decode(t, get(t, s, "/api/config/effective"), &got)
	if got.Error != "" {
		t.Fatalf("error = %q", got.Error)
	}
	values := map[string]string{}
	for _, sec := range got.Sections {
		for _, e := range sec.Entries {
			values[e.Key] = e.Value
		}
	}
	if values["tunnel.enabled"] != "true" {
		t.Errorf("tunnel.enabled = %q, want the file's true", values["tunnel.enabled"])
	}
	if v := values["notifications.slack_webhook"]; v != "(set)" {
		t.Errorf("notifications.slack_webhook = %q, want it masked", v)
	}
	if _, ok := values["verify.enabled"]; !ok {
		t.Errorf("verify.enabled missing: defaults belong on the page too (%d keys)", len(values))
	}
	if len(got.Sources) == 0 {
		t.Error("no sources: the page must say which file the values came from")
	}
	if resp := get(t, newGatedServer(t, &fakeState{}), "/api/config/effective?token=test-token-stakeholder"); resp.StatusCode != http.StatusForbidden {
		t.Errorf("stakeholder: %d, want 403", resp.StatusCode)
	}
}
