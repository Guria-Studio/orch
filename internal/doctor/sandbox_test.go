package doctor

import (
	"errors"
	"strings"
	"testing"
)

// On and unavailable is an error, since orch run refuses to start; off and
// unavailable is skipped; available is ok either way.
func TestCheckSandbox(t *testing.T) {
	noLandlock := errors.New("this kernel has no Landlock")
	cases := []struct {
		name    string
		enabled bool
		abi     int
		err     error
		want    Status
		detail  string
	}{
		{"on, available", true, 6, nil, StatusOK, "ABI 6 confines"},
		{"off, available", false, 6, nil, StatusOK, "available to turn it on"},
		{"on, no landlock", true, 0, noLandlock, StatusError, "no Landlock"},
		{"on, ABI too old", true, 1, nil, StatusError, "ABI 1, the sandbox needs 2"},
		{"off, no landlock", false, 0, noLandlock, StatusSkip, "not available here"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckSandbox(tc.enabled, tc.abi, tc.err, 2)
			if got.Status != tc.want || !strings.Contains(got.Detail, tc.detail) {
				t.Errorf("CheckSandbox = %s %q, want %s containing %q", got.Status, got.Detail, tc.want, tc.detail)
			}
			if got.Status == StatusError && got.Remediation == "" {
				t.Error("an error with no remediation")
			}
		})
	}
}
