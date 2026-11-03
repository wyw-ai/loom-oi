// Package registration turns the OIC configuration into the full
// register snapshot (B1 §4) and finds the channels a reload must abort.
package registration

import (
	"sort"

	"github.com/wyw-ai/loom-oi/internal/oic/config"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// Snapshot lists the enabled exposures sorted by ID; it never carries a
// target address.
func Snapshot(c *config.Config) []protocol.RegisterExposure {
	out := []protocol.RegisterExposure{}
	for _, e := range c.Exposures {
		if !e.IsEnabled() {
			continue
		}
		out = append(out, protocol.RegisterExposure{
			ExposureID: e.ID, Revision: e.Revision, Protocol: e.Protocol,
			TransportProfile: protocol.TransportTCP, AdmissionMode: e.AdmissionMode, Description: e.Description,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExposureID < out[j].ExposureID })
	return out
}

// Invalidated returns the exposure IDs whose open channels must end with
// RevisionChanged after moving from old to next: removed, disabled or
// revision changed. Description and admission changes keep channels.
func Invalidated(old, next *config.Config) map[string]bool {
	keep := map[string]string{}
	for _, e := range next.Exposures {
		if e.IsEnabled() {
			keep[e.ID] = e.Revision
		}
	}
	out := map[string]bool{}
	for _, e := range old.Exposures {
		if rev, ok := keep[e.ID]; !ok || rev != e.Revision {
			out[e.ID] = true
		}
	}
	return out
}
