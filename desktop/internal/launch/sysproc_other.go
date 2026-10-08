//go:build !windows

package launch

import "os/exec"

func newConsole(*exec.Cmd) {}
