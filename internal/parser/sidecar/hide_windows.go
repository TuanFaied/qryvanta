//go:build windows

package sidecar

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: the desktop app is a GUI program, so without it every
// sidecar start would flash a console window.
const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
