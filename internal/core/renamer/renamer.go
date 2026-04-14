// Package renamer applies naming format templates to produce filesystem-safe
// filenames for downloaded media files.
package renamer

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Default format constants used when no explicit format is configured.
const (
	DefaultEpisodeFormat      = "{Series Title} - S{Season:00}E{Episode:00} - {Episode Title} {Quality Full}"
	DefaultDailyEpisodeFormat = "{Series Title} - {Air Date} - {Episode Title} {Quality Full}"
	DefaultAnimeEpisodeFormat = "{Series Title} - S{Season:00}E{Episode:00} - {Absolute Episode:000} - {Episode Title} {Quality Full}"
	DefaultSeriesFolderFormat = "{Series Title} ({Release Year})"
	DefaultSeasonFolderFormat = "Season {Season:00}"
	DefaultMovieFormat        = "{Movie Title} ({Release Year}) {Quality Full}"
	DefaultMovieFolderFormat  = "{Movie Title} ({Release Year})"
)

// ColonReplacement controls how colons in titles are handled
// when producing filesystem-safe filenames.
type ColonReplacement string

const (
	ColonDelete    ColonReplacement = "delete"
	ColonDash      ColonReplacement = "dash"
	ColonSpaceDash ColonReplacement = "space-dash"
)

// Quality holds the quality info needed for renaming.
type Quality struct {
	Name  string `json:"name"`  // e.g. "Bluray-1080p x265"
	Codec string `json:"codec"` // e.g. "x265"
}

// Series holds series-level metadata the renamer needs.
type Series struct {
	Title         string
	OriginalTitle string
	Year          int
}

// Episode holds episode-level metadata the renamer needs.
type Episode struct {
	SeasonNumber   int
	EpisodeNumber  int
	AbsoluteNumber int
	Title          string
	AirDate        string // "2024-01-15"
}

// Movie holds movie-level metadata the renamer needs.
type Movie struct {
	Title string
	Year  int
}

// ApplyEpisodeFormat returns the formatted filename (without extension).
func ApplyEpisodeFormat(format string, series Series, episode Episode, quality Quality, colon ColonReplacement) string {
	r := strings.NewReplacer(
		"{Series Title}", series.Title,
		"{Series CleanTitle}", CleanTitle(series.Title, colon),
		"{Original Title}", series.OriginalTitle,
		"{Release Year}", yearStr(series.Year),
		"{Season:00}", fmt.Sprintf("%02d", episode.SeasonNumber),
		"{season:00}", fmt.Sprintf("%02d", episode.SeasonNumber),
		"{Episode:00}", fmt.Sprintf("%02d", episode.EpisodeNumber),
		"{episode:00}", fmt.Sprintf("%02d", episode.EpisodeNumber),
		"{Absolute Episode:000}", fmt.Sprintf("%03d", episode.AbsoluteNumber),
		"{Episode Title}", episode.Title,
		"{Air Date}", episode.AirDate,
		"{Air-Date}", episode.AirDate,
		"{Quality Full}", quality.Name,
		"{MediaInfo VideoCodec}", quality.Codec,
		"{Year}", yearStr(series.Year),
	)
	return sanitize(r.Replace(format))
}

// ApplyMovieFormat returns the formatted filename (without extension).
func ApplyMovieFormat(format string, movie Movie, quality Quality, colon ColonReplacement) string {
	r := strings.NewReplacer(
		"{Movie Title}", movie.Title,
		"{Movie CleanTitle}", CleanTitle(movie.Title, colon),
		"{Release Year}", yearStr(movie.Year),
		"{Quality Full}", quality.Name,
		"{MediaInfo VideoCodec}", quality.Codec,
		"{Year}", yearStr(movie.Year),
	)
	return sanitize(r.Replace(format))
}

// ApplyFolderFormat returns the series/movie root folder name.
func ApplyFolderFormat(format string, title string, year int) string {
	r := strings.NewReplacer(
		"{Series Title}", title,
		"{Movie Title}", title,
		"{Series CleanTitle}", CleanTitle(title, ColonSpaceDash),
		"{Movie CleanTitle}", CleanTitle(title, ColonSpaceDash),
		"{Original Title}", title,
		"{Release Year}", yearStr(year),
		"{Year}", yearStr(year),
	)
	return sanitize(r.Replace(format))
}

// ApplySeasonFolderFormat returns the season sub-folder name.
func ApplySeasonFolderFormat(format string, seasonNumber int) string {
	r := strings.NewReplacer(
		"{Season:00}", fmt.Sprintf("%02d", seasonNumber),
		"{season:00}", fmt.Sprintf("%02d", seasonNumber),
	)
	return sanitize(r.Replace(format))
}

// EpisodeDestPath returns the absolute destination path for an episode file.
func EpisodeDestPath(
	root, episodeFormat, seriesFolderFormat, seasonFolderFormat string,
	series Series, episode Episode,
	quality Quality, colon ColonReplacement,
	ext string,
) string {
	seriesDir := ApplyFolderFormat(seriesFolderFormat, series.Title, series.Year)
	seasonDir := ApplySeasonFolderFormat(seasonFolderFormat, episode.SeasonNumber)
	filename := ApplyEpisodeFormat(episodeFormat, series, episode, quality, colon) + ext
	return filepath.Join(root, seriesDir, seasonDir, filename)
}

// MovieDestPath returns the absolute destination path for a movie file.
func MovieDestPath(
	root, movieFormat, movieFolderFormat string,
	movie Movie,
	quality Quality, colon ColonReplacement,
	ext string,
) string {
	movieDir := ApplyFolderFormat(movieFolderFormat, movie.Title, movie.Year)
	filename := ApplyMovieFormat(movieFormat, movie, quality, colon) + ext
	return filepath.Join(root, movieDir, filename)
}

// CleanTitle strips characters that are problematic on common filesystems.
func CleanTitle(title string, colon ColonReplacement) string {
	switch colon {
	case ColonDash:
		title = strings.ReplaceAll(title, ":", "-")
	case ColonSpaceDash:
		title = strings.ReplaceAll(title, ": ", " - ")
		title = strings.ReplaceAll(title, ":", "-")
	default:
		title = strings.ReplaceAll(title, ":", " ")
	}
	title = invalidCharsRe.ReplaceAllString(title, "")
	title = multiSpaceRe.ReplaceAllString(title, " ")
	return strings.TrimSpace(title)
}

func sanitize(s string) string {
	s = strings.NewReplacer("/", "", "\x00", "").Replace(s)
	s = multiSpaceRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

func yearStr(y int) string {
	if y == 0 {
		return ""
	}
	return fmt.Sprintf("%d", y)
}

var (
	invalidCharsRe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	multiSpaceRe   = regexp.MustCompile(`\s{2,}`)
)
