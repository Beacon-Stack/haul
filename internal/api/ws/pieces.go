package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"github.com/beacon-stack/haul/internal/core/torrent"
)

// PieceWatcher streams one torrent's piece changes until ctx ends.
type PieceWatcher func(ctx context.Context, hash string, send func(torrent.PieceUpdate) error) error

// PiecesHandler serves a WebSocket that sends a torrent's pieces as they
// finish, for the detail page's piece bar. The torrent is watched only
// while a page is connected. Like the hub, it relies on the router's
// auth gate, which also rejects cross-site handshakes.
func PiecesHandler(watch PieceWatcher, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hash := chi.URLParam(r, "hash")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
			CompressionMode:    websocket.CompressionContextTakeover,
		})
		if err != nil {
			logger.Error("ws: pieces upgrade failed", "error", err)
			return
		}

		// CloseRead answers pings and cancels ctx when the page goes away.
		ctx := conn.CloseRead(r.Context())

		err = watch(ctx, hash, func(u torrent.PieceUpdate) error {
			data, err := json.Marshal(u)
			if err != nil {
				return err
			}
			wCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			defer cancel()
			return conn.Write(wCtx, websocket.MessageText, data)
		})
		if err != nil && ctx.Err() == nil {
			conn.Close(websocket.StatusInternalError, "piece stream ended")
			return
		}
		conn.Close(websocket.StatusNormalClosure, "")
	}
}
