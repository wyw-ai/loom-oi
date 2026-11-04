package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
)

// logResourceFDs is observer-side diagnostics, never a verdict or a new
// baseline. Reading a child's /proc directory adds no descriptor to that child.
// Missing/transient links remain explicit and cannot become a zero counter.
func (e *env) logResourceFDs(stage, checkID, profileID string) {
	if runtime.GOOS != "linux" {
		return
	}
	roles := map[string]*proc{"ois": e.ois}
	for access, oic := range e.oics {
		if oic != nil {
			roles["oic:"+access] = oic.proc
		}
	}
	names := make([]string, 0, len(roles))
	for name := range roles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, role := range names {
		p := roles[role]
		fields := map[string]any{"stage": stage, "check_id": checkID, "profile_id": profileID, "role": role,
			"sampled_ns": resourceNS(), "diagnostic_only": true}
		if p == nil || p.cmd == nil || p.cmd.Process == nil {
			fields["error"] = "process identity missing"
			e.obs("fd_diagnostic", fields)
			continue
		}
		pid := p.cmd.Process.Pid
		fields["pid"] = pid
		dir := fmt.Sprintf("/proc/%d/fd", pid)
		entries, err := os.ReadDir(dir)
		if err != nil {
			fields["error"] = err.Error()
		} else {
			links, failures := map[string]string{}, map[string]string{}
			for _, entry := range entries {
				link, err := os.Readlink(filepath.Join(dir, entry.Name()))
				if err != nil {
					failures[entry.Name()] = err.Error()
				} else {
					links[entry.Name()] = link
				}
			}
			fields["links"], fields["link_errors"] = links, failures
		}
		e.obs("fd_diagnostic", fields)
	}
}
