package torrent

import "regexp"

// Resolution tags we recognise in release names. The four buckets cover
// effectively everything we see — anime, sports, and obscure releases
// occasionally use other tags but the long tail isn't worth modelling.
//
// The regex is deliberately strict: we want a token boundary on both
// sides so "S1080P11" doesn't get tagged as 1080p and "X264" doesn't
// pollute SD detection. Case-insensitive.
var (
	res2160 = regexp.MustCompile(`(?i)(^|[^0-9a-z])(2160p|4k|uhd)([^0-9a-z]|$)`)
	res1080 = regexp.MustCompile(`(?i)(^|[^0-9a-z])1080p([^0-9a-z]|$)`)
	res720  = regexp.MustCompile(`(?i)(^|[^0-9a-z])720p([^0-9a-z]|$)`)
	res480  = regexp.MustCompile(`(?i)(^|[^0-9a-z])(480p|sd)([^0-9a-z]|$)`)
)

// parseResolution returns the canonical resolution tag for a torrent
// name, or "" when none of the recognised buckets match. Tested against
// the highest tier first so a name carrying multiple tokens (rare —
// "1080p.upscale.from.4k") gets the highest one.
func parseResolution(name string) string {
	switch {
	case res2160.MatchString(name):
		return "2160p"
	case res1080.MatchString(name):
		return "1080p"
	case res720.MatchString(name):
		return "720p"
	case res480.MatchString(name):
		return "480p"
	}
	return ""
}
