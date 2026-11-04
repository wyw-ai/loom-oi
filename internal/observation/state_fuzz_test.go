package observation

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// FuzzObservationState drives public reducer operations in arbitrary orders.
// It checks irreversible expiry, immutable sample lifetimes, admission and
// stale-work isolation independently of the scheduler or control transport.
func FuzzObservationState(f *testing.F) {
	for _, seed := range [][]byte{{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, {0, 2, 0, 4, 255, 3}, {0, 5, 2, 6, 3}, {0, 2, 9, 9, 9}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 256 {
			input = input[:256]
		}
		table, b, now := testTable(t)
		plan := testPlan(t)
		generation := int64(1)
		var work []WorkToken
		finished := map[WorkToken]bool{}
		type identity struct {
			binding Binding
			seq     uint64
		}
		deadlines := map[identity]time.Time{}
		expired := map[identity]bool{}
		updates := map[Binding]uint64{}
		for i, op := range input {
			now = now.Add(time.Duration(op) * 7 * time.Millisecond)
			switch op % 10 {
			case 0, 1:
				token, _, err := table.Begin(b, now)
				if err == nil {
					work = append(work, token)
				} else if !errors.Is(err, ErrBusy) && !errors.Is(err, ErrNoProbe) {
					t.Fatal(err)
				}
			case 2, 3, 4:
				if len(work) == 0 {
					break
				}
				token := work[(i+int(op))/3%len(work)]
				before, _, err := table.Snapshot(b, now)
				if err != nil {
					t.Fatal(err)
				}
				out := Outcome{Readiness: Ready, Reason: MatchedReady}
				if op%10 == 3 {
					out = Outcome{Readiness: NotReady, Reason: MatchedNotReady}
				} else if op%10 == 4 {
					out = Outcome{Readiness: Unknown, Reason: Timeout}
				}
				effect, err := table.Finish(token, out, now)
				if err != nil {
					t.Fatal(err)
				}
				if token.Binding != b || finished[token] {
					after, _, err := table.Snapshot(b, now)
					if err != nil || !reflect.DeepEqual(before, after) || effect != (Effect{}) {
						t.Fatal("stale work changed the current identity", before, after, effect, err)
					}
				}
				finished[token] = true
			case 5:
				p := plan
				if op&16 != 0 {
					p = nil
				}
				var err error
				b, _, err = table.Set(b.ExposureID, b.Revision, p, now)
				if err != nil {
					t.Fatal(err)
				}
			case 6:
				generation++
				if _, err := table.Reset(Session{"lab-a", 1, generation}); err != nil {
					t.Fatal(err)
				}
				var err error
				b, _, err = table.Set("lab-a/http", "rv_one", plan, now)
				if err != nil {
					t.Fatal(err)
				}
			case 7:
				if _, err := table.Remove(b.ExposureID); err != nil {
					t.Fatal(err)
				}
				var err error
				b, _, err = table.Set("lab-a/http", "rv_one", plan, now)
				if err != nil {
					t.Fatal(err)
				}
			}
			snap, _, err := table.Snapshot(b, now)
			if err != nil || snap.UpdateSeq < updates[b] || table.Identities() > 2 {
				t.Fatal("identity/sequence bound", snap, err)
			}
			updates[b] = snap.UpdateSeq
			if sample := snap.LastSample; sample != nil {
				key := identity{b, sample.Seq}
				if old, ok := deadlines[key]; ok && old != sample.ExpiresAt {
					t.Fatal("same sample lifetime was renewed")
				}
				deadlines[key] = sample.ExpiresAt
				if snap.Readiness != Unknown {
					if expired[key] || sample.ExpiresAt.Sub(now) < time.Millisecond || snap.Remaining > sample.ExpiresAt.Sub(now) {
						t.Fatal("expired sample revived or lifetime extended", snap)
					}
				} else if snap.Reason == Expired {
					expired[key] = true
				}
			}
			for _, mode := range []Mode{BestEffort, RequireReady} {
				d, _, err := table.Evaluate(b, mode, now)
				want := snap.Readiness == Ready || mode == BestEffort && snap.Readiness == Unknown
				if err != nil || d.Allowed != want {
					t.Fatal("admission bypass", mode, snap, d, err)
				}
			}
		}
	})
}
