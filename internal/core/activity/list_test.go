package activity

import (
	"strings"
	"testing"
)

func TestBuildListQuery_DefaultsAndShape(t *testing.T) {
	listSQL, countSQL, args := buildListQuery(ListFilter{})
	if len(args) != 0 {
		t.Fatalf("expected no args for empty filter, got %v", args)
	}
	if !strings.Contains(listSQL, "FROM torrents") {
		t.Fatalf("expected FROM torrents, got: %s", listSQL)
	}
	// Default sort = added_at DESC, default limit = 50.
	if !strings.Contains(listSQL, "ORDER BY added_at DESC") {
		t.Fatalf("expected default ORDER BY added_at DESC, got: %s", listSQL)
	}
	if !strings.Contains(listSQL, "LIMIT 50") {
		t.Fatalf("expected default LIMIT 50, got: %s", listSQL)
	}
	if !strings.Contains(countSQL, "SELECT COUNT(*)") {
		t.Fatalf("expected count select, got: %s", countSQL)
	}
}

func TestBuildListQuery_SearchInjectsParameterizedILike(t *testing.T) {
	listSQL, _, args := buildListQuery(ListFilter{Search: "ubuntu"})
	if !strings.Contains(listSQL, "ILIKE") {
		t.Fatalf("expected ILIKE clause, got: %s", listSQL)
	}
	// Must use a placeholder, NOT inline the search string into SQL —
	// that would be an injection vector.
	if strings.Contains(listSQL, "ubuntu") {
		t.Fatalf("search term must be parameterised, found inline in: %s", listSQL)
	}
	if len(args) != 1 || args[0] != "%ubuntu%" {
		t.Fatalf("expected single arg %%ubuntu%%, got %v", args)
	}
}

func TestBuildListQuery_StatusFilters(t *testing.T) {
	cases := []struct {
		status   string
		wantSnip string
	}{
		{"active", "removed_at IS NULL AND completed_at IS NULL"},
		{"completed", "completed_at IS NOT NULL AND removed_at IS NULL"},
		{"removed", "removed_at IS NOT NULL"},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			sql, _, _ := buildListQuery(ListFilter{Status: tc.status})
			if !strings.Contains(sql, tc.wantSnip) {
				t.Fatalf("status=%s: expected %q in SQL, got: %s", tc.status, tc.wantSnip, sql)
			}
		})
	}
}

func TestBuildListQuery_StatusAllOrEmptyAddsNoClause(t *testing.T) {
	for _, s := range []string{"", "all"} {
		sql, _, _ := buildListQuery(ListFilter{Status: s})
		// "removed_at" and "completed_at" appear in the SELECT list
		// even with no status filter — only the WHERE-clause forms
		// indicate a status filter was applied.
		if strings.Contains(sql, "removed_at IS") || strings.Contains(sql, "completed_at IS") {
			t.Fatalf("status=%q should not add a status clause, got: %s", s, sql)
		}
	}
}

func TestBuildListQuery_SortAllowlist(t *testing.T) {
	// Each known column must end up in the ORDER BY. resolution is
	// special — it's mapped to a CASE expression for tier-based
	// ordering, but the column name itself must still appear.
	for col := range validSortColumns {
		sql, _, _ := buildListQuery(ListFilter{Sort: col})
		if col == "resolution" {
			if !strings.Contains(sql, "CASE resolution") {
				t.Fatalf("sort=resolution should map to CASE expression, got: %s", sql)
			}
			continue
		}
		if !strings.Contains(sql, "ORDER BY "+col) {
			t.Fatalf("sort=%s missing from ORDER BY: %s", col, sql)
		}
	}

	// Unknown column must fall back to added_at — never splice user
	// input into ORDER BY directly.
	sql, _, _ := buildListQuery(ListFilter{Sort: "drop_table"})
	if !strings.Contains(sql, "ORDER BY added_at") {
		t.Fatalf("unknown sort should fall back to added_at, got: %s", sql)
	}
	if strings.Contains(sql, "drop_table") {
		t.Fatalf("unknown sort column leaked into SQL: %s", sql)
	}
}

func TestBuildListQuery_ResolutionSortIsTierMapped(t *testing.T) {
	// 480p → 1, 720p → 2, 1080p → 3, 2160p → 4. ASC must surface SD
	// first; DESC must surface 4K first. This is what the user
	// expects when they click "Quality" in the table — alphabetical
	// sort would put 1080p before 2160p, which is wrong.
	asc, _, _ := buildListQuery(ListFilter{Sort: "resolution", Order: "asc"})
	for _, snippet := range []string{
		"WHEN '480p' THEN 1",
		"WHEN '720p' THEN 2",
		"WHEN '1080p' THEN 3",
		"WHEN '2160p' THEN 4",
		" ASC NULLS LAST",
	} {
		if !strings.Contains(asc, snippet) {
			t.Fatalf("resolution ASC missing %q in: %s", snippet, asc)
		}
	}
}

func TestBuildListQuery_OrderAscDesc(t *testing.T) {
	asc, _, _ := buildListQuery(ListFilter{Order: "asc"})
	if !strings.Contains(asc, " ASC NULLS LAST") {
		t.Fatalf("expected ASC, got: %s", asc)
	}

	desc, _, _ := buildListQuery(ListFilter{Order: "desc"})
	if !strings.Contains(desc, " DESC NULLS LAST") {
		t.Fatalf("expected DESC, got: %s", desc)
	}

	// Unknown order falls back to DESC.
	junk, _, _ := buildListQuery(ListFilter{Order: "sideways"})
	if !strings.Contains(junk, " DESC NULLS LAST") {
		t.Fatalf("unknown order should fall back to DESC, got: %s", junk)
	}
}

func TestBuildListQuery_LimitClamping(t *testing.T) {
	// Below the floor.
	sql1, _, _ := buildListQuery(ListFilter{Limit: 0})
	if !strings.Contains(sql1, "LIMIT 50") {
		t.Fatalf("expected default LIMIT 50, got: %s", sql1)
	}

	// Above the ceiling — must clamp to 200, no matter how large the input.
	sql2, _, _ := buildListQuery(ListFilter{Limit: 9999})
	if !strings.Contains(sql2, "LIMIT 200") {
		t.Fatalf("expected clamp to LIMIT 200, got: %s", sql2)
	}

	// In range.
	sql3, _, _ := buildListQuery(ListFilter{Limit: 25})
	if !strings.Contains(sql3, "LIMIT 25") {
		t.Fatalf("expected LIMIT 25, got: %s", sql3)
	}
}

func TestBuildListQuery_OffsetNonNegative(t *testing.T) {
	sql, _, _ := buildListQuery(ListFilter{Offset: -1})
	if !strings.Contains(sql, "OFFSET 0") {
		t.Fatalf("expected negative offset clamped to 0, got: %s", sql)
	}
}
