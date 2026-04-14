package torrent

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/beacon-stack/haul/internal/events"
)

// HookRunner executes external commands on torrent events.
type HookRunner struct {
	onAdd      string // command to run when torrent is added
	onComplete string // command to run when torrent completes
	logger     *slog.Logger
}

// NewHookRunner creates a hook runner with optional commands.
// Commands support these variables:
//   - %h = info hash
//   - %n = torrent name
//   - %p = content path
//   - %c = category
func NewHookRunner(onAdd, onComplete string, logger *slog.Logger) *HookRunner {
	return &HookRunner{
		onAdd:      onAdd,
		onComplete: onComplete,
		logger:     logger,
	}
}

// HandleEvent implements events.Handler.
func (r *HookRunner) HandleEvent(_ context.Context, e events.Event) {
	var cmd string
	switch e.Type {
	case events.TypeTorrentAdded:
		cmd = r.onAdd
	case events.TypeTorrentCompleted:
		cmd = r.onComplete
	default:
		return
	}

	if cmd == "" {
		return
	}

	// Replace variables.
	name, _ := e.Data["name"].(string)
	path, _ := e.Data["path"].(string)
	category, _ := e.Data["category"].(string)

	expanded := cmd
	expanded = strings.ReplaceAll(expanded, "%h", e.InfoHash)
	expanded = strings.ReplaceAll(expanded, "%n", name)
	expanded = strings.ReplaceAll(expanded, "%p", path)
	expanded = strings.ReplaceAll(expanded, "%c", category)

	go r.run(expanded, e.InfoHash)
}

func (r *HookRunner) run(cmd, hash string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	r.logger.Info("running hook", "command", cmd, "hash", hash)

	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	output, err := c.CombinedOutput()
	if err != nil {
		r.logger.Warn("hook failed", "command", cmd, "hash", hash, "error", err, "output", string(output))
		return
	}

	r.logger.Info("hook completed", "command", cmd, "hash", hash)
}
