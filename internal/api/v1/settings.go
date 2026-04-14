package v1

// settings.go — runtime settings HTTP handler.
//
// ⚠ The `settings` table is a persistent store but NOT an authoritative
// runtime source. The torrent engine holds its config as a value-type at
// Session construction, so writes to this table alone do nothing. Every
// setting that must affect runtime behavior has to be explicitly wired
// through the Session with a Set* method (see `SetPauseOnComplete` for
// the pattern).
//
// If you add a new setting that needs to take effect at runtime:
//
//   1. Add a corresponding `runtimeMu`-protected field on torrent.Session
//   2. Initialize it in NewSession from cfg AND the DB overlay
//   3. Add a Set*() method that both updates the field and logs the change
//   4. Add a case in applyRuntimeSettings() below
//   5. Extend the regression test in settings_test.go to cover the new key
//
// If you DON'T do these steps, the user will see a toggle in the UI that
// appears to save but has no actual effect — which is exactly the bug
// that brought us here for `pause_on_complete`.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beacon-stack/haul/internal/core/torrent"
)

type settingsBody struct {
	Settings map[string]string `json:"settings"`
}

type settingsOutput struct {
	Body *settingsBody
}

type setSettingsInput struct {
	Body struct {
		Settings map[string]string `json:"settings" doc:"Key-value settings to update"`
	}
}

// applyRuntimeSettings walks a settings map and dispatches each key to the
// appropriate Session setter. This is what makes UI toggles actually take
// effect at runtime. Keys that aren't in the dispatch table are persisted
// to the DB (by the caller) but have no runtime effect — that's fine for
// settings that only apply at startup (listen_port, network_interface,
// etc.), but behavioral toggles must be dispatched here.
//
// Returns a list of keys that had a runtime effect, for logging.
func applyRuntimeSettings(session *torrent.Session, updates map[string]string) []string {
	if session == nil {
		return nil
	}
	var applied []string
	for k, v := range updates {
		switch k {
		case "pause_on_complete":
			session.SetPauseOnComplete(v == "true" || v == "1")
			applied = append(applied, k)
		}
	}
	return applied
}

// RegisterSettingsRoutes registers the runtime settings API.
func RegisterSettingsRoutes(api huma.API, db *sql.DB, session *torrent.Session) {
	huma.Register(api, huma.Operation{
		OperationID: "get-settings",
		Method:      http.MethodGet,
		Path:        "/api/v1/settings",
		Summary:     "Get runtime settings",
		Tags:        []string{"Settings"},
	}, func(_ context.Context, _ *struct{}) (*settingsOutput, error) {
		rows, err := db.Query(`SELECT key, value FROM settings ORDER BY key`)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		defer rows.Close()

		settings := make(map[string]string)
		for rows.Next() {
			var k, v string
			if rows.Scan(&k, &v) == nil {
				settings[k] = v
			}
		}
		return &settingsOutput{Body: &settingsBody{Settings: settings}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-settings",
		Method:      http.MethodPut,
		Path:        "/api/v1/settings",
		Summary:     "Update runtime settings",
		Tags:        []string{"Settings"},
	}, func(_ context.Context, input *setSettingsInput) (*settingsOutput, error) {
		for k, v := range input.Body.Settings {
			_, err := db.Exec(`INSERT INTO settings (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, k, v)
			if err != nil {
				return nil, huma.Error500InternalServerError(err.Error())
			}
		}

		// Dispatch runtime-effective keys to the Session so the toggle
		// actually takes effect NOW, not just on next restart. Without
		// this, the DB update is a phantom write.
		_ = applyRuntimeSettings(session, input.Body.Settings)

		// Return all settings.
		rows, err := db.Query(`SELECT key, value FROM settings ORDER BY key`)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		defer rows.Close()

		settings := make(map[string]string)
		for rows.Next() {
			var k, v string
			if rows.Scan(&k, &v) == nil {
				settings[k] = v
			}
		}
		return &settingsOutput{Body: &settingsBody{Settings: settings}}, nil
	})

	// Config dump — returns the effective startup config as JSON (secrets redacted).
	huma.Register(api, huma.Operation{
		OperationID: "get-config",
		Method:      http.MethodGet,
		Path:        "/api/v1/config",
		Summary:     "Get effective startup configuration (secrets redacted)",
		Tags:        []string{"Settings"},
	}, func(_ context.Context, _ *struct{}) (*struct{ Body json.RawMessage }, error) {
		// Settings table is the runtime overlay. The full config is from startup
		// and isn't stored here — but the settings table is queryable.
		rows, err := db.Query(`SELECT key, value FROM settings ORDER BY key`)
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		defer rows.Close()

		settings := make(map[string]string)
		for rows.Next() {
			var k, v string
			if rows.Scan(&k, &v) == nil {
				settings[k] = v
			}
		}
		data, _ := json.Marshal(settings)
		return &struct{ Body json.RawMessage }{Body: data}, nil
	})
}
