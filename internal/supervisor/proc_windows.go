//go:build windows

package supervisor

import (
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess    = kernel32.NewProc("OpenProcess")
	procGetExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")
	procTerminateProc  = kernel32.NewProc("TerminateProcess")
)

const (
	processQueryLimitedInformation = 0x1000
	processTerminate               = 0x0001
	stillActive                    = 259
)

// On Windows there is no SIGTERM; graceful stop of llama-server is best
// effort via TerminateProcess after the grace window elapses in Stop().
func setSysProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000, // DETACHED_PROCESS
	}
}

func openProc(pid int, access uint32) (syscall.Handle, bool) {
	h, _, err := procOpenProcess.Call(uintptr(access), 0, uintptr(pid))
	if h == 0 {
		_ = err
		return 0, false
	}
	return syscall.Handle(h), true
}

func terminate(proc *os.Process) {
	// Graceful: try a WM_CLOSE-free soft terminate after grace; here we do
	// nothing on SIGTERM-equivalent and let Stop() escalate to Kill().
	_ = proc
}

func processAlive(pid int) bool {
	h, ok := openProc(pid, processQueryLimitedInformation)
	if !ok {
		return false
	}
	defer syscall.CloseHandle(h)
	var exitCode uint32
	ret, _, _ := procGetExitCodeProcess.Call(uintptr(h), uintptr(unsafe.Pointer(&exitCode)))
	if ret == 0 {
		return false
	}
	return exitCode == stillActive
}

// killHard force-terminates; used as escalation after the grace period.
func killHard(pid int) {
	h, ok := openProc(pid, processTerminate)
	if !ok {
		return
	}
	procTerminateProc.Call(uintptr(h), 1)
	syscall.CloseHandle(h)
	_ = time.Now
}
