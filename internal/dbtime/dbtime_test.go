package dbtime

import (
	"sort"
	"testing"
	"time"
)

func TestFormat_IsRFC3339AndFixedWidth(t *testing.T) {
	ts := time.Date(2026, 11, 18, 17, 26, 54, 0, time.FixedZone("CET", 3600))
	got := Format(ts)
	if want := "2026-11-18T16:26:54.000Z"; got != want {
		t.Fatalf("Format = %q, want %q (UTC, millisecond, fixed width)", got, want)
	}
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Fatalf("Format output must parse as RFC3339 for Pilot/Prism: %v", err)
	}
}

func TestParse_AcceptsEveryFormatInExistingDatabases(t *testing.T) {
	want := time.Date(2026, 11, 18, 17, 26, 54, 587_000_000, time.UTC)
	cases := map[string]string{
		"current":                "2026-11-18T17:26:54.587Z",
		"time.String UTC":        "2026-11-18 17:26:54.587019257 +0000 UTC",
		"time.String monotonic":  "2026-11-18 17:26:54.587019257 +0000 UTC m=+1234.567890123",
		"time.String other zone": "2026-11-18 18:26:54.587019257 +0100 CET",
		"rfc3339nano":            "2026-11-18T17:26:54.587019257Z",
	}
	for name, in := range cases {
		got, err := Parse(in)
		if err != nil {
			t.Errorf("%s: Parse(%q): %v", name, in, err)
			continue
		}
		if got.Truncate(time.Millisecond) != want {
			t.Errorf("%s: Parse(%q) = %s, want %s", name, in, got, want)
		}
	}

	sec, err := Parse("2026-11-18T17:26:54Z") // removed_at was plain RFC3339
	if err != nil || !sec.Equal(time.Date(2026, 11, 18, 17, 26, 54, 0, time.UTC)) {
		t.Errorf("Parse(plain RFC3339) = %s, %v", sec, err)
	}
	if _, err := Parse("not a time"); err == nil {
		t.Error("Parse(garbage) succeeded")
	}
}

func TestNormalize(t *testing.T) {
	got, ok := Normalize("2026-11-18 17:26:54.587019257 +0000 UTC")
	if !ok || got != "2026-11-18T17:26:54.587Z" {
		t.Fatalf("Normalize = %q, %v", got, ok)
	}
	if got, ok := Normalize("garbage"); ok || got != "garbage" {
		t.Fatalf("Normalize(garbage) = %q, %v; want unchanged, false", got, ok)
	}
	if !IsCanonical(got) && IsCanonical("2026-11-18T17:26:54Z") {
		t.Fatal("IsCanonical wrong")
	}
}

// Fixed width means text order is time order, so ORDER BY added_at works.
func TestFormat_SortsChronologically(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	times := []time.Time{
		base.Add(1500 * time.Millisecond), base, base.Add(time.Second),
		base.Add(10 * time.Hour), base.Add(999 * time.Millisecond),
	}
	strs := make([]string, len(times))
	for i, ts := range times {
		strs[i] = Format(ts)
	}
	sort.Strings(strs)
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	for i := range times {
		if strs[i] != Format(times[i]) {
			t.Fatalf("text order differs from time order at %d: %v", i, strs)
		}
	}
}
