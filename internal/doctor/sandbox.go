package doctor

import "fmt"

// CheckSandbox says whether dispatch.sandbox can confine agents on this
// machine (#481). abi and abiErr are sandbox.ABI's answer; minABI is
// sandbox.MinABI. On and unavailable is an error, because `orch run` refuses
// to start; off, it is ok when the kernel could enforce it and skipped when
// not, so a machine without Landlock is not nagged about a feature it left
// off.
func CheckSandbox(enabled bool, abi int, abiErr error, minABI int) Check {
	const name = "sandbox.landlock"
	problem := ""
	switch {
	case abiErr != nil:
		problem = abiErr.Error()
	case abi < minABI:
		problem = fmt.Sprintf("this kernel's Landlock is ABI %d, the sandbox needs %d (Linux 5.19+)", abi, minABI)
	}
	switch {
	case problem == "" && enabled:
		return Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf("dispatch.sandbox is on; Landlock ABI %d confines each agent", abi)}
	case problem == "":
		return Check{Name: name, Status: StatusOK, Detail: fmt.Sprintf("dispatch.sandbox is off; Landlock ABI %d is available to turn it on", abi)}
	case enabled:
		return Check{Name: name, Status: StatusError, Detail: "dispatch.sandbox is on but " + problem,
			Remediation: "orch run refuses to start: set dispatch.sandbox: false, or run on a Linux kernel with Landlock"}
	}
	return Check{Name: name, Status: StatusSkip, Detail: "dispatch.sandbox is off, and not available here: " + problem}
}
