//go:build linux

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// helperEnv makes the test binary act as the launcher: it confines itself to
// the policy in the environment and execs /bin/sh with the script, as
// `orch sandbox-exec` does.
const helperEnv = "ORCH_SANDBOX_TEST_HELPER"

func TestMain(m *testing.M) {
	if script := os.Getenv(helperEnv); script != "" {
		p := Policy{
			RW: filepath.SplitList(os.Getenv(helperEnv + "_RW")),
			RO: filepath.SplitList(os.Getenv(helperEnv + "_RO")),
		}
		err := Exec(p, []string{"/bin/sh", "-c", script})
		fmt.Fprintln(os.Stderr, err)
		os.Exit(126)
	}
	os.Exit(m.Run())
}

// A confined command writes inside its allowed directory and is refused a
// file outside it, for reading and for writing — the real kernel, not the
// path list.
func TestExecConfinesTheCommand(t *testing.T) {
	if err := Available(); err != nil {
		t.Skipf("Landlock is not available here, so nothing to confine with: %v", err)
	}
	allowed, denied := t.TempDir(), t.TempDir()
	secret := filepath.Join(denied, "id_ed25519")
	if err := os.WriteFile(secret, []byte("PRIVATE KEY"), 0o600); err != nil {
		t.Fatal(err)
	}
	ro := []string{"/usr", "/bin", "/lib", "/lib64", "/etc"}
	script := fmt.Sprintf(`echo ok > %[1]s/made && echo written
cat %[2]s && echo read-secret
echo x > %[3]s/planted && echo planted
true`, allowed, secret, denied)

	cmd := exec.Command(os.Args[0], "-test.run=^$") // #nosec G204 G702 -- the test binary itself, as the launcher
	cmd.Env = append(os.Environ(), helperEnv+"="+script,
		helperEnv+"_RW="+allowed, helperEnv+"_RO="+strings.Join(ro, string(filepath.ListSeparator)))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the confined command failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "written") {
		t.Errorf("writing the allowed directory failed:\n%s", out)
	}
	if strings.Contains(string(out), "PRIVATE KEY") || strings.Contains(string(out), "read-secret") {
		t.Errorf("the denied file was read:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(denied, "planted")); err == nil || strings.Contains(string(out), "planted\n") {
		t.Errorf("a file was written outside the allowed directory:\n%s", out)
	}
}
