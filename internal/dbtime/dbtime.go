// Package dbtime is the one format Haul uses for timestamps stored in
// SQLite TEXT columns.
//
// Never bind a time.Time directly as a query argument: the SQLite driver
// stores it as time.Time.String() ("2026-11-18 17:26:54.587019257 +0000
// UTC"), which is not RFC3339 and doesn't round-trip through time.Parse,
// so Pilot and Prism could not read it.
//
// The stored format is fixed-width UTC with millisecond precision,
// "2006-01-02T15:04:05.000Z". It is valid RFC3339 (Go's time.RFC3339 and
// RFC3339Nano both parse it), it is exactly the ISO format every
// browser's Date parses, and because it is fixed-width it sorts correctly
// as text, so ORDER BY on these columns stays chronological.
package dbtime

import (
	"fmt"
	"strings"
	"time"
)

// Layout is the stored format. Always UTC; the trailing Z is literal.
const Layout = "2006-01-02T15:04:05.000Z"

// legacyLayouts are formats found in existing databases: time.Time.String()
// output written by older Haul versions, and plain RFC3339 (removed_at).
var legacyLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999 -0700 MST",
}

// Format returns t in the stored format.
func Format(t time.Time) string {
	return t.UTC().Format(Layout)
}

// Parse reads a stored timestamp in the current format or any format
// older Haul versions wrote. The result is in UTC.
func Parse(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	// time.Time.String() appends the monotonic clock reading
	// (" m=+12.345") when the value still had one.
	if i := strings.Index(s, " m="); i >= 0 {
		s = s[:i]
	}
	if t, err := time.Parse(Layout, s); err == nil {
		return t, nil
	}
	for _, layout := range legacyLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised timestamp %q", s)
}

// Normalize rewrites a stored timestamp into the current format. ok is
// false when s can't be parsed; s is then returned unchanged.
func Normalize(s string) (out string, ok bool) {
	t, err := Parse(s)
	if err != nil {
		return s, false
	}
	return Format(t), true
}

// IsCanonical reports whether s is already in the stored format.
func IsCanonical(s string) bool {
	t, err := time.Parse(Layout, s)
	return err == nil && Format(t) == s
}
