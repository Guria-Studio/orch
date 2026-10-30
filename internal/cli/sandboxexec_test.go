package cli

import (
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/Guria-Studio/orch/internal/sandbox"
)

// orch sandbox-exec hands sandbox.Exec the --rw and --ro paths in order and
// the command after --, flags in it included, and maps a failed exec to 126.
func TestSandboxExecParsesThePolicyAndCommand(t *testing.T) {
	var gotP sandbox.Policy
	var gotArgv []string
	orig := sandboxExec
	sandboxExec = func(p sandbox.Policy, argv []string) error {
		gotP, gotArgv = p, argv
		return errors.New("stub: nothing exec'd")
	}
	t.Cleanup(func() { sandboxExec = orig })

	cmd := newSandboxExecCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"--rw", "/w", "--ro", "/usr", "--rw", "/db", "--", "claude", "-p", "--rw", "x"})
	err := cmd.Execute()
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 126 {
		t.Errorf("err = %v, want exit code 126", err)
	}
	if want := []string{"/w", "/db"}; !slices.Equal(gotP.RW, want) {
		t.Errorf("RW = %v, want %v", gotP.RW, want)
	}
	if want := []string{"/usr"}; !slices.Equal(gotP.RO, want) {
		t.Errorf("RO = %v, want %v", gotP.RO, want)
	}
	if want := []string{"claude", "-p", "--rw", "x"}; !slices.Equal(gotArgv, want) {
		t.Errorf("argv = %v, want %v", gotArgv, want)
	}
}
