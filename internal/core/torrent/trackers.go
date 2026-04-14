package torrent

// DefaultPublicTrackers is a curated list of reliable public trackers.
// Magnets from public indexers often arrive with zero tracker URLs, making
// metadata resolution depend entirely on DHT — which is slow and unreliable
// behind VPNs. Adding these trackers dramatically speeds up peer discovery.
//
// Sources: ngosang/trackerslist, newtrackon.com
// Prefer HTTPS/HTTP trackers over UDP for better VPN compatibility.
var DefaultPublicTrackers = [][]string{
	// Tier 1 — fast, reliable HTTPS trackers
	{
		"https://tracker.opentrackr.org:443/announce",
		"https://tracker.torrent.eu.org:443/announce",
		"https://tracker1.bt.moack.co.kr:443/announce",
	},
	// Tier 2 — reliable UDP trackers
	{
		"udp://tracker.opentrackr.org:1337/announce",
		"udp://open.demonii.com:1337/announce",
		"udp://open.stealth.si:80/announce",
		"udp://tracker.torrent.eu.org:451/announce",
		"udp://exodus.desync.com:6969/announce",
	},
	// Tier 3 — additional fallback trackers
	{
		"udp://tracker.moeking.me:6969/announce",
		"udp://explodie.org:6969/announce",
		"udp://tracker1.bt.moack.co.kr:80/announce",
		"udp://tracker.tiny-vps.com:6969/announce",
	},
}
