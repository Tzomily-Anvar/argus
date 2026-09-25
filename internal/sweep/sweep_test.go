package sweep

import (
	"testing"
	"time"
)

// The overnight backoff is one decision - which interval to wait now -
// and everything else is plumbing around it, so that decision is what
// is tested, as a function of when someone last looked.
func TestInterval(t *testing.T) {
	const configured = 15 * time.Minute
	now := time.Date(2026, 3, 9, 8, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		last time.Time
		want time.Duration
	}{
		{"just asked", now, configured},
		{"asked during the working day", now.Add(-2 * time.Hour), configured},
		{"a minute short of idle", now.Add(-idleAfter + time.Minute), configured},
		{"exactly idle", now.Add(-idleAfter), configured * idleFactor},
		{"overnight", now.Add(-14 * time.Hour), configured * idleFactor},
		{"all weekend", now.Add(-60 * time.Hour), configured * idleFactor},
		{"never asked", time.Time{}, configured * idleFactor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Interval(tc.last, now, configured); got != tc.want {
				t.Errorf("Interval(last %s ago) = %s, want %s", now.Sub(tc.last), got, tc.want)
			}
		})
	}
}

// A process that has just started has had no request yet, and must not
// start slow because of it: the seed is the start time, so the first
// wait is the configured interval.
func TestNewStartsAwake(t *testing.T) {
	c := New(nil, 15*time.Minute)
	if got := c.currentInterval(); got != 15*time.Minute {
		t.Fatalf("fresh cache waits %s, want the configured 15m", got)
	}
}

// The first request after a long silence is the one that wakes the
// sweep: it queues an immediate sweep and the next wait is the
// configured interval again. A request during the day queues nothing,
// or every page load would cost a sweep.
func TestTouchWakes(t *testing.T) {
	c := New(nil, 15*time.Minute)

	c.Touch()
	select {
	case <-c.trigger:
		t.Fatal("a request while awake queued a sweep")
	default:
	}

	c.mu.Lock()
	c.lastRequest = time.Now().Add(-10 * time.Hour)
	c.mu.Unlock()
	if got := c.currentInterval(); got != 60*time.Minute {
		t.Fatalf("after ten idle hours the wait is %s, want 1h", got)
	}

	c.Touch()
	select {
	case <-c.trigger:
	default:
		t.Fatal("the first request after backing off did not queue a sweep")
	}
	if got := c.currentInterval(); got != 15*time.Minute {
		t.Fatalf("after waking the wait is %s, want the configured 15m", got)
	}
}
