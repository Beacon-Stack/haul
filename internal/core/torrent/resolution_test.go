package torrent

import "testing"

func TestParseResolution(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Headline cases — every supported tier with a real-looking name.
		{"4K UHD remux", "Dune.2021.UHD.BluRay.2160p.HEVC.Atmos-FraMeSToR", "2160p"},
		{"4K alias", "Movie.Name.4K.HDR.WEB-DL", "2160p"},
		{"UHD alias", "Movie.UHD.WEB-DL.x265", "2160p"},
		{"1080p webrip", "Show.S01E01.1080p.WEB-DL.x264", "1080p"},
		{"720p hdtv", "Show.S01E02.720p.HDTV.x264", "720p"},
		{"480p sd", "Old.Show.S01E01.480p.DVDRip.XviD", "480p"},
		{"SD alias", "Old.Show.SD.WEBRip", "480p"},

		// Highest-tier-wins on ambiguous names.
		{"2160p outranks 1080p", "Movie.2160p.upscale.from.1080p.WEB", "2160p"},

		// Negative cases — token boundaries matter.
		{"no resolution", "Show.S01E01.WEB-DL.x264", ""},
		{"random number string", "Track 11 - 1080 percent done", ""},
		// "X264" used to false-match older patterns; verify we don't tag it.
		{"x264 not 480p", "Show.S01.x264.HEVC", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseResolution(tc.in)
			if got != tc.want {
				t.Fatalf("parseResolution(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
