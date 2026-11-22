package v1

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2/humatest"
)

func TestSystemStatus(t *testing.T) {
	_, api := humatest.New(t)
	start := time.Now().Add(-90 * time.Second)
	RegisterSystemRoutes(api, start, "/config/haul.db")

	resp := api.Get("/api/v1/system/status")
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var body systemStatusBody
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.AppName != "Haul" || body.DBType != "sqlite" || body.DBPath != "/config/haul.db" || body.Version == "" || body.GoVersion == "" {
		t.Errorf("unexpected body %+v", body)
	}
	if body.UptimeSeconds < 90 || body.StartTime != start.UTC().Format(time.RFC3339) {
		t.Errorf("uptime %d, start %s", body.UptimeSeconds, body.StartTime)
	}
}
