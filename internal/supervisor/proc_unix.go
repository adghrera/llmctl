//go:build !windows

package supervisor

import (
	"os"
	"os/exec"
	"syscall"
)

func setSysProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminate(proc *os.Process) { proc.Signal(syscall.SIGTERM) }

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
