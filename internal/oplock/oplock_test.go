package oplock

import (
	"path/filepath"
	"testing"
)

func TestOneHolderAtATime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "op")
	a, ok, err := TryLock(path, "crew merge 12")
	if err != nil || !ok {
		t.Fatalf("first: %v %v", ok, err)
	}
	if got := Holder(path); got != "crew merge 12" {
		t.Fatalf("holder %q", got)
	}
	if _, ok, err := TryLock(path, "crew ticket start 3"); err != nil || ok {
		t.Fatalf("second while held: %v %v", ok, err)
	}
	if err := a.Release(); err != nil {
		t.Fatal(err)
	}
	b, ok, err := TryLock(path, "crew ticket start 3")
	if err != nil || !ok {
		t.Fatalf("after release: %v %v", ok, err)
	}
	defer b.Release()
	if got := Holder(path); got != "crew ticket start 3" {
		t.Fatalf("holder %q", got)
	}
}
