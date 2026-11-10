//go:build !windows

package cli

import "os/exec"

func hideConsole(cmd *exec.Cmd) {}
