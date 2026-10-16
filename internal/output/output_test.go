package output

import (
	"bytes"
	"testing"
	"time"
)

// #2-AC4
func TestTimeIsAbsoluteUTC(t *testing.T) {
	cest := time.FixedZone("CEST", 2*60*60)
	got := Time(time.Date(2026, 10, 15, 11, 12, 59, 0, cest))
	if want := "2026-10-15T09:12Z"; got != want {
		t.Fatalf("Time() = %q, want %q", got, want)
	}
}

// #2-AC4
func TestJSONIsOneLine(t *testing.T) {
	var b bytes.Buffer
	if err := JSON(&b, map[string]string{"version": "v1.2.3 <a&b>"}); err != nil {
		t.Fatal(err)
	}
	if want := `{"version":"v1.2.3 <a&b>"}` + "\n"; b.String() != want {
		t.Fatalf("JSON() wrote %q, want %q", b.String(), want)
	}
}
