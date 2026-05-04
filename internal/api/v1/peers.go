package v1

// peers.go — sibling-service discovery for the UI.
//
// Haul's Activity page wants to deep-link a torrent back to the
// service that requested it (Pilot for episodes, Prism for movies).
// To build the link, the browser needs the sibling's base URL, which
// is registered with Pulse but not visible to the frontend.
//
// This endpoint asks the Pulse SDK once per request (cheap — Pulse's
// /services/discover is a tiny in-memory list) and returns a flat
// {service_name → api_url} map. Empty result is fine: when Pulse is
// down or Haul is running standalone, the frontend just renders
// non-clickable badges.

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beacon-stack/haul/internal/pulse"
)

type peerServicesOutput struct {
	Body struct {
		// Services is a map from registered service NAME ("pilot",
		// "prism", …) to that service's APIURL. Names are unique
		// in Pulse's registry. Map keeps the wire payload small and
		// gives the UI an O(1) lookup by requester string. API keys
		// are intentionally NOT exposed here — keep them server-side
		// only. Backend handlers that need to call siblings use the
		// in-memory peerCache below, which holds the keys.
		Services map[string]string `json:"services"`
	}
}

// RegisterPeerRoutes wires GET /api/v1/peers. The pulse integration
// is optional; when nil the endpoint returns an empty map instead of
// 500ing — the frontend already handles that case.
func RegisterPeerRoutes(api huma.API, integ *pulse.Integration) {
	huma.Register(api, huma.Operation{
		OperationID: "list-peer-services",
		Method:      http.MethodGet,
		Path:        "/api/v1/peers",
		Summary:     "List sibling Beacon services discovered via Pulse",
		Description: "Returns name→api_url for every service Pulse currently knows about. Used by the Activity page to build deep-links back to Pilot/Prism. Empty when Pulse is unreachable or Haul is running standalone.",
		Tags:        []string{"Discovery"},
	}, func(ctx context.Context, _ *struct{}) (*peerServicesOutput, error) {
		out := &peerServicesOutput{}
		out.Body.Services = map[string]string{}

		if integ == nil || integ.Client == nil {
			return out, nil
		}

		// Cap the discovery call so a slow Pulse can't make the
		// page wait. The frontend retries on its own.
		dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		services, err := integ.Client.DiscoverAll(dctx)
		if err != nil {
			// Log-and-degrade rather than 500 — the frontend renders
			// fine without the deep-link map.
			return out, nil
		}
		for _, s := range services {
			if s.Name == "" || s.APIURL == "" {
				continue
			}
			out.Body.Services[s.Name] = s.APIURL
		}
		return out, nil
	})
}
