//go:build !windows

package sidecar

import "os/exec"

func hideWindow(*exec.Cmd) {}
