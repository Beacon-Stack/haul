package v1

// torrents_add_test.go — covers POST /api/v1/torrents validation and the
// data-URI .torrent upload path. Pairs with the AddTorrent modal in
// web/ui/src/pages/torrents/TorrentList.tsx, which submits picked
// .torrent files as `data:application/x-bittorrent;base64,...` URIs.

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/beacon-stack/haul/internal/config"
	"github.com/beacon-stack/haul/internal/core/torrent"
	"github.com/beacon-stack/haul/internal/events"

	"log/slog"
)

func init() {
	torrent.SetPublicIPDetectTimeoutForTesting(200 * time.Millisecond)
}

func newSessionForAddTest(t *testing.T) *torrent.Session {
	t.Helper()
	cfg := config.TorrentConfig{
		ListenPort:  0,
		DownloadDir: t.TempDir(),
		DataDir:     t.TempDir(),
		EnableDHT:   false,
		EnablePEX:   false,
		EnableUTP:   false,
	}
	bus := events.New(slog.Default())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	session, err := torrent.NewSession(cfg, nil, bus, logger)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	t.Cleanup(session.Close)
	return session
}

func newAddAPI(t *testing.T) (*torrent.Session, humatest.TestAPI) {
	t.Helper()
	session := newSessionForAddTest(t)
	_, api := humatest.New(t)
	RegisterTorrentRoutes(api, session)
	return session, api
}

// makeTorrentBytes builds a minimal valid bencoded .torrent payload using
// anacrolix's metainfo package — same parser the engine uses, so what
// passes here is what passes in production.
func makeTorrentBytes(t *testing.T, name string) []byte {
	t.Helper()
	const pieceLen = 16384
	// One 3-byte file → one piece. Hash of "abc" for stable bytes.
	fileBytes := []byte("abc")
	pieceHash := sha1.Sum(fileBytes)

	info := metainfo.Info{
		Name:        name,
		PieceLength: pieceLen,
		Length:      int64(len(fileBytes)),
		Pieces:      pieceHash[:],
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("bencode info: %v", err)
	}
	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		t.Fatalf("mi.Write: %v", err)
	}
	return buf.Bytes()
}

// ── Validation rejection paths ─────────────────────────────────────────

func TestAddTorrent_RejectsEmptyBody(t *testing.T) {
	_, api := newAddAPI(t)
	resp := api.Post("/api/v1/torrents", map[string]any{"uri": ""})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("Code = %d, want %d; body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "uri or .torrent") {
		t.Errorf("body = %s, want hint about uri or .torrent", resp.Body.String())
	}
}

func TestAddTorrent_RejectsUnsupportedScheme(t *testing.T) {
	_, api := newAddAPI(t)
	resp := api.Post("/api/v1/torrents", map[string]any{"uri": "ftp://example.com/foo.torrent"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("Code = %d, want %d; body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "unsupported uri scheme") {
		t.Errorf("body = %s, want 'unsupported uri scheme'", resp.Body.String())
	}
}

func TestAddTorrent_RejectsBareString(t *testing.T) {
	_, api := newAddAPI(t)
	resp := api.Post("/api/v1/torrents", map[string]any{"uri": "not-a-url"})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("Code = %d, want %d; body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

func TestAddTorrent_RejectsMalformedBase64(t *testing.T) {
	_, api := newAddAPI(t)
	resp := api.Post("/api/v1/torrents", map[string]any{
		"uri": "data:application/x-bittorrent;base64,!!!not-valid-base64!!!",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("Code = %d, want %d; body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "base64") {
		t.Errorf("body = %s, want 'base64' in error", resp.Body.String())
	}
}

func TestAddTorrent_RejectsEmptyDataURI(t *testing.T) {
	_, api := newAddAPI(t)
	resp := api.Post("/api/v1/torrents", map[string]any{
		"uri": "data:application/x-bittorrent;base64,",
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("Code = %d, want %d; body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

func TestAddTorrent_RejectsNonBencodedDataURI(t *testing.T) {
	// Valid base64 of "hello" — passes decode but isn't a bencoded torrent.
	// The handler magic-byte check rejects this before it reaches the engine.
	_, api := newAddAPI(t)
	payload := base64.StdEncoding.EncodeToString([]byte("hello"))
	resp := api.Post("/api/v1/torrents", map[string]any{
		"uri": "data:application/x-bittorrent;base64," + payload,
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("Code = %d, want %d; body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "bencoded") {
		t.Errorf("body = %s, want 'bencoded' in error", resp.Body.String())
	}
}

func TestAddTorrent_RejectsOversizedDataURI(t *testing.T) {
	// Build an 11MB payload with the 'd' magic byte so it passes the
	// bencode prefix check but trips the size cap. We assert that the
	// 10MB cap fires before we hand the payload to the engine.
	_, api := newAddAPI(t)
	oversized := make([]byte, 11*1024*1024)
	oversized[0] = 'd'
	payload := base64.StdEncoding.EncodeToString(oversized)
	resp := api.Post("/api/v1/torrents", map[string]any{
		"uri": "data:application/x-bittorrent;base64," + payload,
	})
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("Code = %d, want %d; body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "size limit") {
		t.Errorf("body = %s, want 'size limit' in error", resp.Body.String())
	}
}

func TestAddTorrent_RejectsBencodedNonTorrent(t *testing.T) {
	// Valid base64 of "de" — bencoded empty dict, passes our magic-byte
	// check, but anacrolix's metainfo.Load rejects it because there's no
	// info dict. Confirms 422 (not 400) since the bytes parsed as bencode
	// but aren't a torrent.
	_, api := newAddAPI(t)
	payload := base64.StdEncoding.EncodeToString([]byte("de"))
	resp := api.Post("/api/v1/torrents", map[string]any{
		"uri": "data:application/x-bittorrent;base64," + payload,
	})
	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Code = %d, want %d; body=%s", resp.Code, http.StatusUnprocessableEntity, resp.Body.String())
	}
}

// ── Accept paths ───────────────────────────────────────────────────────

func TestAddTorrent_AcceptsValidDataURI(t *testing.T) {
	session, api := newAddAPI(t)
	mi := makeTorrentBytes(t, "haul-add-test-fixture")
	payload := base64.StdEncoding.EncodeToString(mi)

	resp := api.Post("/api/v1/torrents", map[string]any{
		"uri":    "data:application/x-bittorrent;base64," + payload,
		"paused": true, // don't try to actually announce/download
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("Code = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}

	var info struct {
		InfoHash string `json:"info_hash"`
		Name     string `json:"name"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode: %v\nbody=%s", err, resp.Body.String())
	}
	if info.Name != "haul-add-test-fixture" {
		t.Errorf("name = %q, want %q", info.Name, "haul-add-test-fixture")
	}
	if len(info.InfoHash) != 40 {
		t.Errorf("info_hash = %q, want 40-char hex", info.InfoHash)
	}

	// And the session's List() must reflect the new torrent — confirms
	// we made it past the in-memory registration and persistTorrent step.
	if got := len(session.List()); got != 1 {
		t.Errorf("session.List() len = %d, want 1", got)
	}
}

func TestAddTorrent_AcceptsValidMagnet(t *testing.T) {
	_, api := newAddAPI(t)
	// A well-formed magnet URI with a stable info hash. Add will succeed
	// at the handler/session boundary even though the torrent will never
	// actually fetch metadata in the isolated test environment.
	resp := api.Post("/api/v1/torrents", map[string]any{
		"uri":    "magnet:?xt=urn:btih:0000000000000000000000000000000000000001&dn=test",
		"paused": true,
	})
	if resp.Code != http.StatusOK {
		t.Fatalf("Code = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
}
