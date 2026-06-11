package renamer

import (
	"testing"
)

func TestApplyEpisodeFormat(t *testing.T) {
	series := Series{Title: "Breaking Bad", Year: 2008}
	ep := Episode{SeasonNumber: 1, EpisodeNumber: 4, Title: "Cancer Man"}
	quality := Quality{Name: "Bluray-1080p"}

	got := ApplyEpisodeFormat(DefaultEpisodeFormat, series, ep, quality, ColonSpaceDash)
	want := "Breaking Bad - S01E04 - Cancer Man Bluray-1080p"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestApplyMovieFormat(t *testing.T) {
	movie := Movie{Title: "Fight Club", Year: 1999}
	quality := Quality{Name: "Bluray-2160p"}

	got := ApplyMovieFormat(DefaultMovieFormat, movie, quality, ColonSpaceDash)
	want := "Fight Club (1999) Bluray-2160p"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCleanTitleColonStrategies(t *testing.T) {
	tests := []struct {
		title string
		colon ColonReplacement
		want  string
	}{
		{"Title: Subtitle", ColonDelete, "Title Subtitle"},
		{"Title: Subtitle", ColonDash, "Title- Subtitle"},
		{"Title: Subtitle", ColonSpaceDash, "Title - Subtitle"},
		{"No Colons Here", ColonSpaceDash, "No Colons Here"},
		{"Multiple: Colons: Here", ColonSpaceDash, "Multiple - Colons - Here"},
	}

	for _, tt := range tests {
		t.Run(tt.title+"_"+string(tt.colon), func(t *testing.T) {
			got := CleanTitle(tt.title, tt.colon)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyFolderFormat(t *testing.T) {
	got := ApplyFolderFormat(DefaultSeriesFolderFormat, "Breaking Bad", 2008)
	if got != "Breaking Bad (2008)" {
		t.Errorf("got %q", got)
	}
}

func TestApplySeasonFolderFormat(t *testing.T) {
	got := ApplySeasonFolderFormat(DefaultSeasonFolderFormat, 3)
	if got != "Season 03" {
		t.Errorf("got %q", got)
	}
}
