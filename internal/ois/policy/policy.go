// Package policy evaluates discover/open grants. A policy matches an exact
// exposure ID or every exposure of one Access ("<access_id>/*").
package policy

import (
	"strings"

	"github.com/wyw-ai/loom-oi/internal/ois/store"
	"github.com/wyw-ai/loom-oi/internal/protocol"
)

// ValidSelector reports whether sel is an exact exposure ID or
// "<access_id>/*".
func ValidSelector(sel string) bool {
	if access, ok := strings.CutSuffix(sel, "/*"); ok {
		return protocol.ValidAccessID(access)
	}
	_, _, ok := protocol.SplitExposureID(sel)
	return ok
}

// Match reports whether sel covers exposureID.
func Match(sel, exposureID string) bool {
	if access, ok := strings.CutSuffix(sel, "/*"); ok {
		a, _, valid := protocol.SplitExposureID(exposureID)
		return valid && a == access
	}
	return sel == exposureID
}

// ValidActions reports whether actions is a non-empty subset of
// discover/open without duplicates.
func ValidActions(actions []string) bool {
	seen := map[string]bool{}
	for _, a := range actions {
		if (a != protocol.ActionDiscover && a != protocol.ActionOpen) || seen[a] {
			return false
		}
		seen[a] = true
	}
	return len(actions) > 0
}

// Allowed reports whether any enabled policy grants action on exposureID
// to subject.
func Allowed(policies []store.Policy, subject, exposureID, action string) bool {
	for _, p := range policies {
		if !p.Enabled || p.Subject != subject || !Match(p.ExposureSelector, exposureID) {
			continue
		}
		for _, a := range p.Actions {
			if a == action {
				return true
			}
		}
	}
	return false
}
