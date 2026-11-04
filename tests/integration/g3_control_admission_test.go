package integration

import (
	"errors"
	"fmt"
	"testing"

	"github.com/wyw-ai/loom-oi/internal/protocol"
)

func TestG3BlackholePreflightResult(t *testing.T) {
	timeout := &protocol.Error{Code: protocol.CodeReadinessCheckFailed, Reason: "timeout"}
	for _, tc := range []struct {
		name   string
		status int
		err    error
		opened protocol.OpenResponse
		ok     bool
	}{
		{name: "timeout", status: 503, err: timeout, ok: true},
		{name: "wrapped_timeout", status: 503, err: fmt.Errorf("request: %w", timeout), ok: true},
		{name: "offline", status: 503, err: &protocol.Error{Code: protocol.CodeAccessOffline}, ok: true},
		{name: "created", status: 201, opened: protocol.OpenResponse{ChannelID: "ch_unexpected"}},
		{name: "missing_error", status: 503},
		{name: "uncoded_error", status: 503, err: errors.New("timeout")},
		{name: "wrong_status", status: 504, err: timeout},
		{name: "missing_reason", status: 503, err: &protocol.Error{Code: protocol.CodeReadinessCheckFailed}},
		{name: "unavailable", status: 503, err: &protocol.Error{Code: protocol.CodeReadinessCheckFailed, Reason: "unavailable"}},
		{name: "changed", status: 503, err: &protocol.Error{Code: protocol.CodeReadinessCheckFailed, Reason: "observation_changed"}},
		{name: "store_failure", status: 503, err: &protocol.Error{Code: protocol.CodeStoreUnavailable}},
		{name: "offline_reason", status: 503, err: &protocol.Error{Code: protocol.CodeAccessOffline, Reason: "timeout"}},
		{name: "channel", status: 503, err: timeout, opened: protocol.OpenResponse{ChannelID: "ch_unexpected"}},
		{name: "attachment", status: 503, err: timeout, opened: protocol.OpenResponse{Attach: &protocol.Attach{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, reason, err := g3BlackholePreflightResult(tc.status, tc.err, tc.opened)
			if (err == nil) != tc.ok {
				t.Fatalf("accepted=%v, want %v: %v", err == nil, tc.ok, err)
			}
			if tc.ok {
				var pe *protocol.Error
				if !errors.As(tc.err, &pe) || code != pe.Code || reason != pe.Reason {
					t.Fatalf("changed original error: %s/%s", code, reason)
				}
			}
		})
	}
}

func TestG3BlackholePreflightNoPending(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*protocol.OISStats)
		ok     bool
	}{
		{"existing_open", func(s *protocol.OISStats) { s.Channels.Open = 1 }, true},
		{"old_stream_closed", func(s *protocol.OISStats) { s.Channels.Closed = 3 }, true},
		{"ticket", func(s *protocol.OISStats) { s.Tickets = 1 }, false},
		{"preparing", func(s *protocol.OISStats) { s.Channels.Preparing = 1 }, false},
		{"missing_observation", func(s *protocol.OISStats) { s.Observation = nil }, false},
		{"pending_check", func(s *protocol.OISStats) { s.Observation.PendingChecks = 1 }, false},
		{"pending_open", func(s *protocol.OISStats) { s.Observation.PendingOpens = 1 }, false},
		{"http_waiter", func(s *protocol.OISStats) { s.Observation.HTTPWaiters = 1 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := resourceProcess[protocol.OISStats]{Stats: protocol.OISStats{Observation: &protocol.ObservationRuntimeStats{}}}
			tc.mutate(&s.Stats)
			if err := g3NoPendingBlackholeOpen(s); (err == nil) != tc.ok {
				t.Fatalf("accepted=%v, want %v: %v", err == nil, tc.ok, err)
			}
		})
	}
}
