//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/landlock-lsm/go-landlock/landlock"
	llsys "github.com/landlock-lsm/go-landlock/landlock/syscall"
)

// ABI is the kernel's Landlock ABI version, or an error saying why there is
// none: an old kernel, or Landlock left out of the boot's lsm= list.
func ABI() (int, error) {
	v, err := llsys.LandlockGetABIVersion()
	if err != nil {
		return 0, fmt.Errorf("this kernel has no Landlock (%v): it needs Linux 5.19+ with landlock in the lsm= boot list", err)
	}
	return v, nil
}

// Available is nil when the sandbox can confine an agent here.
func Available() error {
	v, err := ABI()
	if err != nil {
		return err
	}
	if v < MinABI {
		return fmt.Errorf("this kernel's Landlock is ABI %d, the sandbox needs %d (Linux 5.19+)", v, MinABI)
	}
	return nil
}

// Exec confines this process to p and replaces it with argv. It returns only
// on failure, and then nothing was exec'd: an agent never runs unconfined
// because its sandbox could not be applied.
//
// The ruleset is go-landlock's V8 in best-effort mode, so a kernel with an
// older ABI (2 and up, Available checks) enforces the subset it knows instead
// of failing. V8, not newer: V9 starts handling connect(2) on UNIX sockets,
// which would cut agents off docker.sock.
func Exec(p Policy, argv []string) error {
	if len(argv) == 0 {
		return errors.New("sandbox: no command to run")
	}
	if err := Available(); err != nil {
		return fmt.Errorf("sandbox: %w", err)
	}
	// Resolve before restricting, for an error that names the command.
	bin, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("sandbox: %w", err)
	}
	if err := landlock.V8.BestEffort().RestrictPaths(rules(p)...); err != nil {
		return fmt.Errorf("sandbox: apply landlock: %w", err)
	}
	return syscall.Exec(bin, argv, os.Environ()) // #nosec G204 G702 -- the provider's argv the engine wrapped, exec'd directly, never through a shell
}

// rules turns p into Landlock rules. A directory and a file take different
// rights (directory rights on a file are EINVAL), so each path is stat'ed; a
// missing one is skipped, as not every machine has /lib32 or ~/.codex.
//
// Writable directories also get refer, for git's cross-directory renames,
// and ioctl on devices, for /dev/tty and /dev/pts.
func rules(p Policy) []landlock.Rule {
	var out []landlock.Rule
	for _, path := range p.RW {
		switch kind(path) {
		case kindDir:
			out = append(out, landlock.RWDirs(path).WithRefer().WithIoctlDev())
		case kindFile:
			out = append(out, landlock.RWFiles(path).WithIoctlDev())
		}
	}
	for _, path := range p.RO {
		switch kind(path) {
		case kindDir:
			out = append(out, landlock.RODirs(path))
		case kindFile:
			out = append(out, landlock.ROFiles(path))
		}
	}
	return out
}

type pathKind int

const (
	kindMissing pathKind = iota
	kindDir
	kindFile
)

func kind(path string) pathKind {
	fi, err := os.Stat(path)
	switch {
	case err != nil:
		return kindMissing
	case fi.IsDir():
		return kindDir
	}
	return kindFile
}
