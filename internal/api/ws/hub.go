package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/beacon-stack/haul/internal/events"
)

const (
	sendBufSize  = 32
	writeTimeout = 10 * time.Second
)

// Hub manages connected WebSocket clients and fans events out to them.
type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
	logger  *slog.Logger
}

// NewHub creates a Hub that broadcasts events to connected WebSocket clients.
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		clients: make(map[chan []byte]struct{}),
		logger:  logger,
	}
}

// HandleEvent implements events.Handler.
func (h *Hub) HandleEvent(_ context.Context, e events.Event) {
	data, err := json.Marshal(e)
	if err != nil {
		h.logger.Error("ws: failed to marshal event", "error", err)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- data:
		default:
			h.logger.Warn("ws: client send buffer full — dropping event")
		}
	}
}

// ServeHTTP upgrades the connection to WebSocket.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	isWSUpgrade := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
	isSameOrigin := r.Header.Get("Sec-Fetch-Site") == "same-origin"

	if !isWSUpgrade && !isSameOrigin {
		http.Error(w, `{"status":401,"title":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionContextTakeover,
	})
	if err != nil {
		h.logger.Error("ws: upgrade failed", "error", err)
		return
	}

	send := make(chan []byte, sendBufSize)

	h.mu.Lock()
	h.clients[send] = struct{}{}
	h.mu.Unlock()

	h.logger.Info("ws: client connected", "remote", r.RemoteAddr)

	defer func() {
		h.mu.Lock()
		delete(h.clients, send)
		h.mu.Unlock()
		close(send)
		h.logger.Info("ws: client disconnected", "remote", r.RemoteAddr)
	}()

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			_, _, err := conn.Read(r.Context())
			if err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-readDone:
			conn.Close(websocket.StatusNormalClosure, "")
			return
		case <-r.Context().Done():
			conn.Close(websocket.StatusGoingAway, "server shutting down")
			return
		case data, ok := <-send:
			if !ok {
				conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			wCtx, cancel := context.WithTimeout(r.Context(), writeTimeout)
			err := conn.Write(wCtx, websocket.MessageText, data)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
