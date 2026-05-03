package v1

// admin_diagnostics.go — Settings → System → Diagnostics endpoints.
//
// Routes registered ONLY when HAUL_ADMIN_DIAGNOSTICS_ENABLED=true. When
// the flag is false, RegisterAdminDiagnosticsRoutes is never called and
// every /api/v1/admin/diagnostics/* path 404s.
//
// All endpoints share the existing X-Api-Key auth (same as the rest of
// the API). The flag is the gate; once flipped on, they're as
// privileged as any other admin route.

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	adminpkg "github.com/beacon-stack/haul/internal/db/admin"
)

// ── Schemas ───────────────────────────────────────────────────────────

type diagnosticSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	RowCount    int    `json:"row_count"`
}

type diagnosticListOutput struct {
	Body []diagnosticSummary
}

type diagnosticDetailInput struct {
	Name string `path:"name" doc:"Diagnostic name (e.g. orphan_torrents)"`
}

type diagnosticDetailOutput struct {
	Body struct {
		Name string         `json:"name"`
		Rows []adminpkg.Row `json:"rows"`
	}
}

type diagnosticCleanupInput struct {
	Name string `path:"name" doc:"Diagnostic name"`
	Body struct {
		IDs  []string `json:"ids,omitempty"  doc:"Specific IDs to clean up. Mutually exclusive with all=true"`
		All  bool     `json:"all,omitempty"  doc:"Clean up every row currently matching the diagnostic"`
		Mode string   `json:"mode,omitempty" doc:"soft (default, recoverable for retention window) or hard (permanent)"`
	}
}

type diagnosticCleanupOutput struct {
	Body adminpkg.CleanupResult
}

// ── Registration ──────────────────────────────────────────────────────

// RegisterAdminDiagnosticsRoutes wires the diagnostics endpoints.
// Caller is responsible for only invoking this when the flag is on —
// the routes are unconditionally registered once this is called.
func RegisterAdminDiagnosticsRoutes(api huma.API, registry *adminpkg.Registry) {
	huma.Register(api, huma.Operation{
		OperationID: "list-diagnostics",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/diagnostics",
		Summary:     "List available diagnostics with current row counts",
		Tags:        []string{"Admin"},
	}, func(ctx context.Context, _ *struct{}) (*diagnosticListOutput, error) {
		out := &diagnosticListOutput{}
		for _, d := range registry.List() {
			rows, err := d.Detect(ctx)
			if err != nil {
				registry.Logger().Warn("diagnostic detect failed during list", "diagnostic", d.Name(), "error", err)
				continue
			}
			out.Body = append(out.Body, diagnosticSummary{
				Name:        d.Name(),
				Description: d.Description(),
				RowCount:    len(rows),
			})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-diagnostic",
		Method:      http.MethodGet,
		Path:        "/api/v1/admin/diagnostics/{name}",
		Summary:     "Run a diagnostic and return matching rows",
		Tags:        []string{"Admin"},
	}, func(ctx context.Context, input *diagnosticDetailInput) (*diagnosticDetailOutput, error) {
		d := registry.Get(input.Name)
		if d == nil {
			return nil, huma.Error404NotFound("unknown diagnostic: " + input.Name)
		}
		rows, err := d.Detect(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("detect failed: " + err.Error())
		}
		out := &diagnosticDetailOutput{}
		out.Body.Name = d.Name()
		out.Body.Rows = rows
		if rows == nil {
			out.Body.Rows = []adminpkg.Row{} // never null in JSON
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "cleanup-diagnostic",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/diagnostics/{name}/cleanup",
		Summary:     "Delete the rows matched by a diagnostic (soft by default, hard via mode=hard)",
		Tags:        []string{"Admin"},
	}, func(ctx context.Context, input *diagnosticCleanupInput) (*diagnosticCleanupOutput, error) {
		d := registry.Get(input.Name)
		if d == nil {
			return nil, huma.Error404NotFound("unknown diagnostic: " + input.Name)
		}
		mode, err := adminpkg.ParseMode(input.Body.Mode)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		if !input.Body.All && len(input.Body.IDs) == 0 {
			return nil, huma.Error400BadRequest("provide ids or all=true")
		}
		req := adminpkg.CleanupRequest{
			IDs:  input.Body.IDs,
			All:  input.Body.All,
			Mode: mode,
		}
		result, err := d.Cleanup(ctx, req)
		if err != nil {
			return nil, huma.Error500InternalServerError("cleanup failed: " + err.Error())
		}
		registry.Logger().Warn("db_cleanup",
			"event", "db_cleanup",
			"diagnostic", d.Name(),
			"mode", mode,
			"rows_deleted", result.RowsDeleted,
			"all", input.Body.All)
		return &diagnosticCleanupOutput{Body: result}, nil
	})
}
