package clock

import (
	"testing"
	"time"
)

// #2-AC5
func TestFakeAfterFiresOnlyWhenAdvancedPastDeadline(t *testing.T) {
	c := NewFake(Epoch)
	ch := c.After(10 * time.Minute)

	c.Advance(10*time.Minute - time.Second)
	select {
	case <-ch:
		t.Fatal("fired before the deadline")
	default:
	}

	c.Advance(time.Second)
	select {
	case got := <-ch:
		if want := Epoch.Add(10 * time.Minute); !got.Equal(want) {
			t.Fatalf("fired at %v, want %v", got, want)
		}
	default:
		t.Fatal("did not fire at the deadline")
	}
	if want := Epoch.Add(10 * time.Minute); !c.Now().Equal(want) {
		t.Fatalf("Now() = %v, want %v", c.Now(), want)
	}
}

// #2-AC4
func TestRealAfterSendsUTC(t *testing.T) {
	if got := <-(Real{}).After(time.Millisecond); got.Location() != time.UTC {
		t.Fatalf("After sent %v, want UTC", got.Location())
	}
}

func TestFakeBlockUntilWaitsForTimers(t *testing.T) {
	c := NewFake(Epoch)
	done := make(chan struct{})
	go func() {
		c.BlockUntil(2)
		close(done)
	}()
	c.After(time.Second)
	select {
	case <-done:
		t.Fatal("returned with one timer")
	default:
	}
	c.After(time.Second)
	<-done
	c.Advance(time.Second)
	c.BlockUntil(0)
}
