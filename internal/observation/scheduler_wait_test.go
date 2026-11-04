package observation

import (
	"testing"
	"time"
)

func TestSchedulerWaitIncludesLateTimerAndPendingOverdue(t *testing.T) {
	s, clock, entered, finished := schedulerFixture(t, 1, 1, 2)
	job := schedulerJob(t, "late")
	if err := s.Update(job); err != nil {
		t.Fatal(err)
	}
	first := receive(t, entered)
	// Advance only the clock, holding its callback. The pending age must
	// include a scheduled deadline even before the delayed timer is invoked.
	clock.mu.Lock()
	clock.now = first.started.Add(job.Plan.Interval() + 3*time.Second)
	clock.mu.Unlock()
	if got := s.Stats().OldestDueAge; got != 3*time.Second {
		t.Fatalf("hidden pending timer age: %v", got)
	}
	clock.Advance(0)
	if got := s.Stats().OldestDueAge; got != 3*time.Second {
		t.Fatalf("timer reset overdue age: %v", got)
	}
	close(first.release)
	receive(t, finished)
	second := receive(t, entered)
	if second.wait != 3*time.Second || s.Stats().MaxWait != 3*time.Second {
		t.Fatalf("late callback hid queue delay: actual %v stats %+v", second.wait, s.Stats())
	}
	close(second.release)
	receive(t, finished)
}
