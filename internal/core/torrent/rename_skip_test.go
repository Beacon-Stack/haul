package torrent

import "testing"

// TestShouldSkipRenameForRequester pins the contract that prevents the
// double-rename race when an arr (Pilot/Prism) requested the torrent.
// The list is exact-match, case-sensitive — Pilot/Prism stamp the
// requester field with one of these literals verbatim, and a fuzzy
// match would either over-skip (e.g. catching "Pilot Light" if a
// custom integration ever stamped that) or under-skip (e.g. missing
// "PILOT" if someone uppercased it). Locking exact matches makes the
// behavior predictable for both the maintainer and the operator.
func TestShouldSkipRenameForRequester(t *testing.T) {
	cases := []struct {
		requester string
		want      bool
		why       string
	}{
		// Headline cases — the two arrs whose import pipelines
		// own the rename step. Skipping here prevents the file path
		// from changing out from under their importer mid-flight.
		{"pilot", true, "Pilot does its own rename + import"},
		{"prism", true, "Prism does its own rename + import"},

		// Standalone or third-party callers — the rename_on_complete
		// toggle is meant for them. Don't skip.
		{"manual", false, "user added directly via the Haul UI"},
		{"", false, "empty requester (legacy or pre-metadata)"},

		// Defensive: exact-match means casing matters. Pilot/Prism
		// always stamp lowercase per RequesterMetadata, so an upper-
		// case value indicates a foreign caller and should NOT be
		// silently treated as Pilot.
		{"Pilot", false, "wrong case — not stamped by the official Pilot client"},
		{"PRISM", false, "wrong case — not stamped by the official Prism client"},

		// Defensive: substring matching would be wrong here.
		{"pilot-staging", false, "fuzzy match must NOT trigger; only exact 'pilot' is the official requester"},
	}

	for _, tc := range cases {
		t.Run(tc.requester+"_"+tc.why, func(t *testing.T) {
			got := shouldSkipRenameForRequester(tc.requester)
			if got != tc.want {
				t.Errorf("shouldSkipRenameForRequester(%q) = %v, want %v (%s)",
					tc.requester, got, tc.want, tc.why)
			}
		})
	}
}
