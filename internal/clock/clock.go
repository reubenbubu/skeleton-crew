// Package clock supplies the current time and timers. Every command takes a
// Clock instead of calling the time package, so tests can use a Fake and
// never sleep.
package clock

import (
	"sync"
	"time"
)

// Clock is the source of time for everything in crew.
type Clock interface {
	Now() time.Time
	// After sends the current time once d has elapsed. Polling loops wait on
	// it with an interval that the caller sets.
	After(d time.Duration) <-chan time.Time
}

// Real is the system clock.
type Real struct{}

func (Real) Now() time.Time                         { return time.Now().UTC() }
func (Real) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	time.AfterFunc(d, func() { ch <- time.Now().UTC() })
	return ch
}

// Epoch is a fixed start time for fakes in tests.
var Epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Fake is a clock that moves only when Advance is called.
type Fake struct {
	mu      sync.Mutex
	now     time.Time
	waiters []waiter
}

type waiter struct {
	at time.Time
	ch chan time.Time
}

func NewFake(now time.Time) *Fake { return &Fake{now: now} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- f.now
		return ch
	}
	f.waiters = append(f.waiters, waiter{at: f.now.Add(d), ch: ch})
	return ch
}

// Advance moves the clock forward by d and fires every timer that is due.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
	pending := f.waiters[:0]
	for _, w := range f.waiters {
		if w.at.After(f.now) {
			pending = append(pending, w)
			continue
		}
		w.ch <- f.now
	}
	f.waiters = pending
}
