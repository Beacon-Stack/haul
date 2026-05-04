package v1

// research.go — server-side proxy for "Re-search via Pilot/Prism".
//
// User clicks the right-click menu item on a stalled torrent. The
// frontend can't call Pilot directly (cross-origin + we don't expose
// API keys to the browser), so this Haul-side endpoint:
//
//   1. Looks up the torrent's requester metadata from history.
//   2. Asks Pulse for the sibling service's URL + API key (via the
//      SDK's DiscoverAll).
//   3. Calls the sibling's /api/v1/grabs/by-hash/{hash}/research
//      endpoint with that key.
//   4. Returns the result body to the frontend.
//
// The sibling endpoint already removes the dead torrent + blocklists
// the dead release + grabs an alternative + submits to Haul. From the
// user's perspective: one click, dead grab gone, new grab downloading.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beacon-stack/haul/internal/core/torrent"
	"github.com/beacon-stack/haul/internal/pulse"
)

type researchInput struct {
	Hash string `path:"hash" doc:"BitTorrent info hash of the stalled torrent"`
}

type researchOutput struct {
	Body struct {
		// Mirrors Pilot/Prism's autoSearchResultBody so the frontend
		// renders the same shape regardless of which sibling answered.
		Result       string `json:"result"`                  // "grabbed" | "no_match"
		ReleaseTitle string `json:"release_title,omitempty"`
		Reason       string `json:"reason,omitempty"`
	}
}

// RegisterResearchRoutes wires POST /api/v1/torrents/{hash}/research.
// Requires both the torrent session (to look up requester) and the
// pulse integration (for sibling discovery + auth). When either is
// missing, the endpoint returns a meaningful error rather than 500ing.
func RegisterResearchRoutes(api huma.API, session *torrent.Session, integ *pulse.Integration) {
	huma.Register(api, huma.Operation{
		OperationID: "research-torrent",
		Method:      http.MethodPost,
		Path:        "/api/v1/torrents/{hash}/research",
		Summary:     "Re-search the requesting service for an alternative release",
		Description: "Looks up the torrent's requester (Pilot/Prism), asks them to blocklist this release, remove the dead torrent, and grab an alternative. Only works when the torrent was originally requested by a Beacon manager service.",
		Tags:        []string{"Torrents"},
	}, func(ctx context.Context, in *researchInput) (*researchOutput, error) {
		// 1. Look up the torrent's requester from the durable history.
		records, err := session.LookupHistory(ctx, torrent.HistoryFilter{
			InfoHash:       in.Hash,
			IncludeRemoved: true,
			Limit:          1,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if len(records) == 0 {
			return nil, huma.Error404NotFound("no history record for that info_hash")
		}
		rec := records[0]
		if rec.Requester == "" {
			return nil, huma.Error400BadRequest("torrent has no requester — re-search is only available for Pilot/Prism-requested grabs")
		}

		// 2. Discover the sibling service via Pulse, including its
		// API key. Uses Pulse's HTTP /services endpoint directly
		// because the pinned SDK's Service struct doesn't surface
		// api_key yet.
		if integ == nil || integ.Client == nil {
			return nil, huma.Error503ServiceUnavailable("Pulse integration not configured — Haul can't reach Pilot/Prism")
		}
		services, err := integ.DiscoverWithKeys(ctx)
		if err != nil {
			return nil, huma.Error503ServiceUnavailable("Pulse unreachable: " + err.Error())
		}
		var apiURL, apiKey string
		for _, s := range services {
			if s.Name == rec.Requester {
				apiURL = s.APIURL
				apiKey = s.APIKey
				break
			}
		}
		if apiURL == "" {
			return nil, huma.Error503ServiceUnavailable(fmt.Sprintf("Pulse doesn't know about %q — is it registered?", rec.Requester))
		}
		if apiKey == "" {
			return nil, huma.Error503ServiceUnavailable(fmt.Sprintf("Pulse has no API key for %q — re-register the service to share its key", rec.Requester))
		}

		// 3. Server-side call to the sibling's research endpoint.
		out, callErr := callSiblingResearch(ctx, apiURL, apiKey, in.Hash)
		if callErr != nil {
			return nil, huma.NewError(http.StatusBadGateway, "sibling research call failed", callErr)
		}
		ret := &researchOutput{}
		ret.Body.Result = out.Result
		ret.Body.ReleaseTitle = out.ReleaseTitle
		ret.Body.Reason = out.Reason
		return ret, nil
	})
}

type siblingResearchResult struct {
	Result       string `json:"result"`
	ReleaseTitle string `json:"release_title,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// callSiblingResearch is a thin HTTP client for Pilot/Prism's
// /api/v1/grabs/by-hash/{hash}/research endpoint. The contract is
// stable across both services because they share the same handler
// implementation in pilot/internal/api/v1/releases.go (Prism mirrors).
func callSiblingResearch(ctx context.Context, baseURL, apiKey, infoHash string) (*siblingResearchResult, error) {
	url := baseURL + "/api/v1/grabs/by-hash/" + infoHash + "/research"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call sibling: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("sibling does not know this info_hash")
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("sibling returned %d: %s", resp.StatusCode, string(body))
	}

	var out siblingResearchResult
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode sibling response: %w (body=%s)", err, string(body))
	}
	return &out, nil
}
