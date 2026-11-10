//go:build linux

package procgroup

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func prepareDetachedLeaf(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func processAlive(pid int) bool {
	b, e := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if e != nil {
		return false
	}
	i := strings.LastIndexByte(string(b), ')')
	if i < 0 {
		return false
	}
	parts := strings.Fields(string(b)[i+1:])
	return len(parts) > 0 && parts[0] != "Z"
}
