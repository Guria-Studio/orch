//go:build !linux

package sandbox

import (
	"errors"
	"runtime"
)

var errUnsupported = errors.New("the agent sandbox is Linux only (Landlock); " + runtime.GOOS + " has no backend yet")

// ABI has no answer off Linux.
func ABI() (int, error) { return 0, errUnsupported }

// Available is never nil off Linux.
func Available() error { return errUnsupported }

// Exec refuses off Linux rather than run the agent unconfined.
func Exec(Policy, []string) error { return errUnsupported }
