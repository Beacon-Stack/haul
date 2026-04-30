package torrent

// rename_test.go — pure-logic tests for the rename helpers.
// renameCompleted and renameTorrentFolder need a real Session +
// anacrolix torrent + RequesterMetadata to exercise; they're covered
// indirectly by the integration test suite. The two pure-Go helpers
// below have zero coverage and govern user-visible behavior:
//
//   - uniquePath collision-suffixing: any bug here silently overwrites
//     existing files (data loss) or produces wrong filenames.
//   - isMediaExt: dictates which files get renamed at all; a missing
//     extension means subtitles get left next to original-named videos
//     (subtitle plugins lose track), and an over-broad extension
//     renames .nfo / .txt that media servers expect to find by name.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── uniquePath ───────────────────────────────────────────────────────────────

func TestUniquePath_NonExistentReturnsInput(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Movie (2024).mkv")

	got := uniquePath(p)
	if got != p {
		t.Errorf("non-existent path: got %q, want %q (no suffix should be appended)", got, p)
	}
}

func TestUniquePath_FirstCollisionAddsParenOne(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Movie (2024).mkv")
	if err := os.WriteFile(p, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := uniquePath(p)
	want := filepath.Join(dir, "Movie (2024) (1).mkv")
	if got != want {
		t.Errorf("first collision: got %q, want %q", got, want)
	}
}

// Multiple collisions must increment past (1) — the function uses
// `Sprintf("%s (%d)%s", base, i, ext)` where i is 1..99. If the loop
// is broken (e.g. fixed at 1) every additional collision overwrites
// the same destination, silently losing files.
func TestUniquePath_SequentialCollisionsIncrement(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "Movie (2024)")

	// Existing files: "Movie (2024).mkv", "Movie (2024) (1).mkv",
	// "Movie (2024) (2).mkv". The next collision should land at (3).
	for _, suffix := range []string{".mkv", " (1).mkv", " (2).mkv"} {
		if err := os.WriteFile(base+suffix, []byte("existing"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := uniquePath(base + ".mkv")
	want := base + " (3).mkv"
	if got != want {
		t.Errorf("third collision: got %q, want %q", got, want)
	}
}

// Path with no extension (e.g. a directory inside renameTorrentFolder)
// still needs collision handling. The TrimSuffix(ext) when ext=="" must
// leave the base intact — `Sprintf("%s (1)%s")` becomes `path (1)`.
func TestUniquePath_NoExtension(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Movie 2024 Folder")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}

	got := uniquePath(p)
	want := p + " (1)"
	if got != want {
		t.Errorf("no-extension collision: got %q, want %q", got, want)
	}
}

// Multi-extension files (e.g. `.tar.gz`, `.subtitle.en.srt`) — the
// implementation uses filepath.Ext which returns ONLY the last
// component, so `Movie (2024).en.srt` collides at
// `Movie (2024).en (1).srt`. Pin that behavior so a future
// "smart double-extension" change doesn't silently break it for
// callers that already adapted.
func TestUniquePath_MultiExtensionUsesLastComponent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Movie (2024).en.srt")
	if err := os.WriteFile(p, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := uniquePath(p)
	want := filepath.Join(dir, "Movie (2024).en (1).srt")
	if got != want {
		t.Errorf("multi-extension: got %q, want %q", got, want)
	}
}

// The 100-collision give-up case: if every candidate (1) through (99)
// already exists, the function returns the original path. The caller
// then attempts os.Rename to a name that collides, which fails — that's
// the safe failure mode (no silent overwrite). Pin this so a future
// change doesn't quietly truncate to (101) etc.
func TestUniquePath_GivesUpAfter99(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "x")

	// Seed existing: "x.mkv" through "x (99).mkv" — 100 files total.
	if err := os.WriteFile(base+".mkv", []byte("e"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 99; i++ {
		// Use the exact same fmt the SUT uses so the test isn't fooled
		// by a formatting drift.
		f := strings.Replace(base+" ($I).mkv", "$I", itoa(i), 1)
		if err := os.WriteFile(f, []byte("e"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := uniquePath(base + ".mkv")
	if got != base+".mkv" {
		t.Errorf("give-up case: got %q, want original %q", got, base+".mkv")
	}
}

// Helper to avoid pulling strconv just for the give-up test.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	out := ""
	for i > 0 {
		out = string(rune('0'+i%10)) + out
		i /= 10
	}
	return out
}

// ── isMediaExt ───────────────────────────────────────────────────────────────

func TestIsMediaExt(t *testing.T) {
	// Spot-check across categories: video, audio, subtitle, plus
	// case-insensitivity and rejected types. This locks the contract
	// renameCompleted relies on to skip nfo/txt files.
	cases := []struct {
		ext  string
		want bool
	}{
		// Video — common
		{".mkv", true},
		{".mp4", true},
		{".avi", true},
		{".webm", true},
		// Video — case-insensitive
		{".MKV", true},
		{".Mkv", true},
		// Audio
		{".mp3", true},
		{".flac", true},
		// Subtitles — must be true so they get renamed alongside video
		// (mismatched basename → some plugins lose subtitle linkage).
		{".srt", true},
		{".sub", true},
		{".ass", true},
		{".idx", true},
		// Rejected: NFO is referenced by media-server scrapers via the
		// torrent's exact original name, so renaming it would break
		// metadata pickup.
		{".nfo", false},
		{".txt", false},
		{".jpg", false},
		{".png", false},
		// Rejected: no extension
		{"", false},
		{".", false},
	}

	for _, c := range cases {
		if got := isMediaExt(c.ext); got != c.want {
			t.Errorf("isMediaExt(%q) = %v, want %v", c.ext, got, c.want)
		}
	}
}
