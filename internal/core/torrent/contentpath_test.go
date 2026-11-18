package torrent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// addNamedTorrent adds a multi-file torrent with the given name and its
// save path set to saveDir, and waits until its metadata is available.
func addNamedTorrent(t *testing.T, s *Session, saveDir, name string) string {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "episode.mkv"), bytes.Repeat([]byte("x"), 32*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	info := metainfo.Info{PieceLength: 16 * 1024}
	if err := info.BuildFromFilePath(src); err != nil {
		t.Fatalf("build info: %v", err)
	}
	info.Name = name
	ib, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := (&metainfo.MetaInfo{InfoBytes: ib}).Write(&buf); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := s.Add(ctx, AddRequest{File: buf.Bytes(), SavePath: saveDir, Paused: true})
	if err != nil {
		t.Fatalf("Session.Add: %v", err)
	}
	s.mu.RLock()
	mt := s.torrents[res.InfoHash]
	s.mu.RUnlock()
	select {
	case <-mt.t.GotInfo():
	case <-ctx.Done():
		t.Fatal("timed out waiting for torrent info")
	}
	return res.InfoHash
}

// The torrent name is attacker-controlled. contentPath must never hand
// back the save dir itself (or anything outside it) as "the torrent's own
// folder", because Remove(deleteFiles=true) RemoveAll's that path.
func TestContentPathForName(t *testing.T) {
	save := "/downloads/tv"
	cases := []struct {
		name   string
		want   string
		wantOK bool
	}{
		{"Show.S01E01.1080p", "/downloads/tv/Show.S01E01.1080p", true},
		{"movie.mkv", "/downloads/tv/movie.mkv", true},
		{"nested/dir", "/downloads/tv/nested/dir", true},
		{"/abs", "/downloads/tv/abs", true}, // storage joins it under the dir too
		{".", save, false},                  // files land straight in save
		{"", save, false},
		{"./", save, false},
		{"a/..", save, false},
		{"..", save, false},
		{"../escape", save, false},
		{"a/../../escape", save, false},
	}
	for _, c := range cases {
		got, ok := contentPathForName(save, c.name)
		if got != c.want || ok != c.wantOK {
			t.Errorf("contentPathForName(%q, %q) = (%q, %v), want (%q, %v)",
				save, c.name, got, ok, c.want, c.wantOK)
		}
	}
}

func TestTorrentContentPath_NilInfo(t *testing.T) {
	if got, ok := torrentContentPath("/d", nil); got != "/d" || ok {
		t.Errorf("torrentContentPath(nil info) = (%q, %v), want (\"/d\", false)", got, ok)
	}
}

// Headline regression: a torrent whose name doesn't give it a folder of
// its own ("./", "a/..", ...) must not take the rest of the save
// directory with it when removed with delete_files=true.
func TestRemove_DeleteFiles_UnsafeNameKeepsSaveDir(t *testing.T) {
	for _, name := range []string{".", "./", "a/..", "x/../"} {
		t.Run(name, func(t *testing.T) {
			s := newTestSession(t)
			saveDir := t.TempDir()
			bystander := filepath.Join(saveDir, "Other.Show.S02E03.mkv")
			if err := os.WriteFile(bystander, []byte("someone else's download"), 0o644); err != nil {
				t.Fatal(err)
			}

			hash := addNamedTorrent(t, s, saveDir, name)
			if err := s.Remove(context.Background(), hash, true); err != nil {
				t.Fatalf("Remove: %v", err)
			}

			if _, err := os.Stat(bystander); err != nil {
				t.Fatalf("Remove(deleteFiles) of a torrent named %q deleted other data in the save dir "+
					"(internal/core/torrent/session.go Remove must use torrentContentPath and refuse when !ok): %v", name, err)
			}
		})
	}
}

// The fix must not break the normal case: removing with delete_files
// still deletes the torrent's own folder, and only that.
func TestRemove_DeleteFiles_NormalNameDeletesOwnFolderOnly(t *testing.T) {
	s := newTestSession(t)
	saveDir := t.TempDir()
	bystander := filepath.Join(saveDir, "Other.Show.S02E03.mkv")
	if err := os.WriteFile(bystander, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	const name = "Show.S01.1080p"
	hash := addNamedTorrent(t, s, saveDir, name)
	own := filepath.Join(saveDir, name)
	if err := os.MkdirAll(own, 0o755); err != nil {
		t.Fatal(err)
	}
	// Not episode.mkv: storage has that file mmapped, and truncating it
	// under the mapping crashes the test with SIGBUS.
	if err := os.WriteFile(filepath.Join(own, "extra.nfo"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Remove(context.Background(), hash, true); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(own); !os.IsNotExist(err) {
		t.Fatalf("torrent's own folder %s still exists after Remove(deleteFiles=true): %v", own, err)
	}
	if _, err := os.Stat(bystander); err != nil {
		t.Fatalf("Remove deleted a file outside the torrent's folder: %v", err)
	}
}

// Moving a torrent with no folder of its own would rename the whole save
// dir. SetLocation must refuse up front, before it drops the torrent or
// attempts any rename, and leave everything where it was. (Without the
// guard it tries `rename <saveDir> <newPath>`; whether that happens to
// fail depends on the destination, so assert on the refusal itself.)
func TestSetLocation_UnsafeNameRefused(t *testing.T) {
	s := newTestSession(t)
	saveDir := t.TempDir()
	bystander := filepath.Join(saveDir, "Other.Show.S02E03.mkv")
	if err := os.WriteFile(bystander, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash := addNamedTorrent(t, s, saveDir, "./")

	err := s.SetLocation(hash, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "folder of its own") {
		t.Fatalf("SetLocation on a torrent named \"./\" = %v; want the up-front refusal "+
			"(internal/core/torrent/control.go must check torrentContentPath before Drop)", err)
	}
	if _, err := os.Stat(bystander); err != nil {
		t.Fatalf("refused SetLocation still moved other data out of the save dir: %v", err)
	}
	if _, err := s.Get(hash); err != nil {
		t.Fatal("refused SetLocation dropped the torrent from the session")
	}
}
