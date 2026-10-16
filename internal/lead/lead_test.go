package lead

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/reubenbubu/skeleton-crew/internal/clock"
)

// #6-AC1: of many claims at the same moment, exactly one wins.
func TestConcurrentClaimsOneWins(t *testing.T) {
	for range 20 {
		root := t.TempDir()
		var wg sync.WaitGroup
		results := make([]error, 8)
		for k := range results {
			wg.Go(func() { _, _, results[k] = Claim(root, clock.Epoch, false) })
		}
		wg.Wait()
		won := 0
		for _, err := range results {
			var held *HeldError
			switch {
			case err == nil:
				won++
			case !errors.As(err, &held):
				t.Fatal(err)
			}
		}
		if won != 1 {
			t.Fatalf("%d claims won", won)
		}
	}
}

// #6-AC1 #6-AC4: of many claims over an unreadable lock, exactly one wins.
func TestConcurrentClaimsOverUnreadableLockOneWins(t *testing.T) {
	for range 50 {
		root := t.TempDir()
		os.MkdirAll(filepath.Join(root, ".crew"), 0o755)
		os.WriteFile(filepath.Join(root, Path), []byte("garbage"), 0o644)
		var wg sync.WaitGroup
		tokens := make([]string, 8)
		for k := range tokens {
			wg.Go(func() {
				l, _, err := Claim(root, clock.Epoch, false)
				var held *HeldError
				if err != nil && !errors.As(err, &held) && err != errChanged {
					t.Error(err)
				}
				tokens[k] = l.Token
			})
		}
		wg.Wait()
		var won []string
		for _, tok := range tokens {
			if tok != "" {
				won = append(won, tok)
			}
		}
		if len(won) != 1 {
			t.Fatalf("%d claims won", len(won))
		}
		if _, err := Check(root, won[0]); err != nil {
			t.Fatalf("the winner is not the lead: %v", err)
		}
	}
}

// #6-AC5: a release by a lead that was taken over leaves the new lead alone.
func TestReleaseAfterTakeoverKeepsTheNewLead(t *testing.T) {
	root := t.TempDir()
	old, _, _ := Claim(root, clock.Epoch, false)
	l, _, _ := Claim(root, clock.Epoch, true)
	var not *NotLeadError
	if _, err := Release(root, old.Token); !errors.As(err, &not) {
		t.Fatalf("got %v", err)
	}
	if _, err := Check(root, l.Token); err != nil {
		t.Fatal(err)
	}
}
