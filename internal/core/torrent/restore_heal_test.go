package torrent

// restore_heal_test.go — Unit tests for the BUG-005 auto-heal path.
//
// The anacrolix-torrent library chmods completed files inside multi-file
// torrents to 0o444 on promotion (storage/file-piece.go:259 in the
// anacrolix source). That breaks every subsequent restart because the
// library's own restore code can't reopen its own read-only files for
// read+write. We catch the AddTorrent permission-denied error in
// restoreFromDB and call healFilesetPermissions to undo the chmod
// before retrying.
//
// The functions under test are pure helpers — they touch the filesystem
// but not anacrolix internals, so the tests can run without a Session
// or DB. The end-to-end "AddTorrent fails → auto-heal → retry succeeds"
// path is exercised live during deploy verification.

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

func TestIsPermissionDenied(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain other", errors.New("disk full"), false},
		{"fs.ErrPermission direct", fs.ErrPermission, true},
		{"fs.ErrPermission wrapped", &os.PathError{Op: "open", Path: "/x", Err: fs.ErrPermission}, true},
		{"substring match anacrolix wrap", errors.New("error opening torrent storage: file \"x.mkv\": open /downloads/x.mkv: permission denied"), true},
		{"substring case-sensitive miss", errors.New("Permission Denied"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPermissionDenied(tc.err); got != tc.want {
				t.Fatalf("isPermissionDenied(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// makeMultiFileMetainfo builds a metainfo describing a torrent with the
// given files. Files are NOT created on disk — the caller decides which
// of them exist and what mode they have.
func makeMultiFileMetainfo(t *testing.T, name string, files []metainfo.FileInfo) *metainfo.MetaInfo {
	t.Helper()
	info := metainfo.Info{
		Name:        name,
		PieceLength: 16 * 1024,
		Files:       files,
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("bencode info: %v", err)
	}
	return &metainfo.MetaInfo{InfoBytes: ib}
}

// makeSingleFileMetainfo builds metainfo for a one-file torrent. Used to
// confirm healFilesetPermissions also handles the single-file path
// (where info.Files is empty and the file lives directly under savePath).
func makeSingleFileMetainfo(t *testing.T, name string, length int64) *metainfo.MetaInfo {
	t.Helper()
	info := metainfo.Info{
		Name:        name,
		PieceLength: 16 * 1024,
		Length:      length,
	}
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("bencode info: %v", err)
	}
	return &metainfo.MetaInfo{InfoBytes: ib}
}

func TestHealFilesetPermissions_MultiFile_ClearsReadOnly(t *testing.T) {
	dir := t.TempDir()
	torrentName := "show-pack"
	root := filepath.Join(dir, torrentName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Two completed (mode 444) files mirror anacrolix's promoted state.
	// One writable file is a control — we should leave it alone.
	completed1 := filepath.Join(root, "E03.mkv")
	completed2 := filepath.Join(root, "E04.mkv")
	writable := filepath.Join(root, "E01.mkv")
	for _, p := range []string{completed1, completed2, writable} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	for _, p := range []string{completed1, completed2} {
		if err := os.Chmod(p, 0o444); err != nil {
			t.Fatalf("chmod 444 %s: %v", p, err)
		}
	}

	mi := makeMultiFileMetainfo(t, torrentName, []metainfo.FileInfo{
		{Path: []string{"E01.mkv"}, Length: 1},
		{Path: []string{"E03.mkv"}, Length: 1},
		{Path: []string{"E04.mkv"}, Length: 1},
	})

	sess := &Session{logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
	healed := sess.healFilesetPermissions(mi, dir, "deadbeef")

	if !healed {
		t.Fatal("healFilesetPermissions returned false, want true (two files needed clearing)")
	}
	for _, p := range []string{completed1, completed2} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if st.Mode().Perm()&0o200 == 0 {
			t.Errorf("%s still missing owner write bit (mode %o)", p, st.Mode().Perm())
		}
	}
	// Control file should be untouched at 0o644.
	st, err := os.Stat(writable)
	if err != nil {
		t.Fatalf("stat control: %v", err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Errorf("control file mode changed: got %o, want 0644", st.Mode().Perm())
	}
}

func TestHealFilesetPermissions_NoReadOnlyFiles_ReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	torrentName := "fresh-torrent"
	root := filepath.Join(dir, torrentName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.mkv"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	mi := makeMultiFileMetainfo(t, torrentName, []metainfo.FileInfo{
		{Path: []string{"a.mkv"}, Length: 1},
	})

	sess := &Session{logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
	if sess.healFilesetPermissions(mi, dir, "abc") {
		t.Fatal("expected false (nothing to heal), got true")
	}
}

func TestHealFilesetPermissions_SingleFile(t *testing.T) {
	dir := t.TempDir()
	const fileName = "movie.mkv"
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, []byte("data"), 0o444); err != nil {
		t.Fatalf("write: %v", err)
	}

	mi := makeSingleFileMetainfo(t, fileName, 4)

	sess := &Session{logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
	if !sess.healFilesetPermissions(mi, dir, "single") {
		t.Fatal("expected healed=true for single-file torrent")
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Mode().Perm()&0o200 == 0 {
		t.Errorf("single file still read-only: mode %o", st.Mode().Perm())
	}
}

func TestHealFilesetPermissions_MissingFile_IsTolerated(t *testing.T) {
	// If a file from the metainfo doesn't exist on disk (partial torrent),
	// healFilesetPermissions should skip it silently rather than erroring.
	// AddTorrent will fail later for a different reason; we don't want this
	// helper to short-circuit the whole restore.
	dir := t.TempDir()
	mi := makeMultiFileMetainfo(t, "x", []metainfo.FileInfo{
		{Path: []string{"never-existed.mkv"}, Length: 1},
	})
	sess := &Session{logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))}
	// No panic, no error — returns false (nothing healed).
	if sess.healFilesetPermissions(mi, dir, "abc") {
		t.Fatal("expected false for missing file, got true")
	}
}
