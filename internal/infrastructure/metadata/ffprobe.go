package metadata

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type ProbeResult struct {
	Format  ProbeFormat  `json:"format"`
	Streams []ProbeStream `json:"streams"`
}

type ProbeFormat struct {
	Filename       string            `json:"filename"`
	FormatName     string            `json:"format_name"`
	Duration       string            `json:"duration"`
	Size           string            `json:"size"`
	BitRate        string            `json:"bit_rate"`
	Tags           map[string]string `json:"tags,omitempty"`
}

type ProbeStream struct {
	CodecType  string `json:"codec_type"`
	CodecName  string `json:"codec_name"`
	SampleRate string `json:"sample_rate"`
	Channels   int    `json:"channels"`
}

type AudioMeta struct {
	Title       string
	Artist      string
	Artists     []string
	AlbumArtist string
	Album       string
	TrackNumber int
	DiscNumber  int
	Year        int
	Genre       string
	Duration    float64
	BitRate     int
	SampleRate  int
	Channels    int
	HasCoverArt bool
	Comment     string
	Composer    string
	Lyricist    string
	Arranger    string
	FilePath    string
	FileSize    int64
	FileFormat  string
	MBID        string
	AcoustID    string

	TitleFromFilename bool
}

var audioExts = map[string]bool{
	".mp3":  true,
	".flac": true,
	".ogg":  true,
	".m4a":  true,
	".wav":  true,
	".dsf":  true,
	".aiff": true,
	".aif":  true,
	".wma":  true,
	".opus": true,
}

func IsAudioFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return audioExts[ext]
}

func Probe(path string) (*AudioMeta, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed on %s: %w", path, err)
	}

	var result ProbeResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output for %s: %w", path, err)
	}

	meta := &AudioMeta{
		FilePath:   path,
		FileFormat: result.Format.FormatName,
	}

	parseFloat(result.Format.Duration, &meta.Duration)
	parseInt(result.Format.BitRate, &meta.BitRate)
	var fileSize int
	parseInt(result.Format.Size, &fileSize)
	meta.FileSize = int64(fileSize)

	for _, s := range result.Streams {
		if s.CodecType == "audio" {
			parseInt(s.SampleRate, &meta.SampleRate)
			meta.Channels = s.Channels
			break
		}
	}

	meta.HasCoverArt = hasCoverArt(result.Streams)

	if result.Format.Tags != nil {
		tags := make(map[string]string, len(result.Format.Tags))
		for k, v := range result.Format.Tags {
			tags[strings.ToLower(k)] = v
		}
		meta.Title = tags["title"]
		meta.Artist = tags["artist"]
		meta.AlbumArtist = tags["album_artist"]
		if meta.AlbumArtist == "" {
			meta.AlbumArtist = tags["albumartist"]
		}
		meta.Album = tags["album"]
		meta.Genre = tags["genre"]
		meta.Comment = tags["comment"]
	meta.Composer = tags["composer"]
	meta.Lyricist = tags["lyricist"]
	meta.Arranger = tags["arranger"]
	meta.MBID = tags["musicbrainz_trackid"]
		if meta.MBID == "" {
			meta.MBID = tags["musicbrainz track id"]
		}

		parseInt(tags["track"], &meta.TrackNumber)
		parseInt(tags["disc"], &meta.DiscNumber)

		if year, ok := tags["date"]; ok && len(year) >= 4 {
			parseInt(year[:4], &meta.Year)
		} else if year, ok := tags["year"]; ok {
			parseInt(year, &meta.Year)
		}
	}

	// Clear garbled fields (invalid UTF-8 or replacement characters)
	if !utf8.ValidString(meta.Title) || strings.ContainsRune(meta.Title, 0xFFFD) {
		meta.Title = ""
	}
	if !utf8.ValidString(meta.Artist) || strings.ContainsRune(meta.Artist, 0xFFFD) {
		meta.Artist = ""
	}
	if !utf8.ValidString(meta.Album) || strings.ContainsRune(meta.Album, 0xFFFD) {
		meta.Album = ""
	}

	meta.Artists = splitArtistNames(meta.Artist)

	meta.TitleFromFilename = meta.Title == ""

	if meta.Title == "" {
		meta.Title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	return meta, nil
}

func hasCoverArt(streams []ProbeStream) bool {
	for _, s := range streams {
		if s.CodecType == "video" {
			return true
		}
	}
	return false
}

func parseFloat(s string, out *float64) {
	if s == "" {
		return
	}
	fmt.Sscanf(s, "%f", out)
}

func parseInt(s string, out *int) {
	if s == "" {
		return
	}
	fmt.Sscanf(s, "%d", out)
}

func splitArtistNames(artist string) []string {
	if artist == "" || artist == "Unknown Artist" {
		return nil
	}
	if strings.Contains(artist, "/") {
		return splitTrim(artist, "/")
	}
	if strings.Contains(artist, ",") {
		return splitTrim(artist, ",")
	}
	if strings.Contains(artist, ";") {
		return splitTrim(artist, ";")
	}
	return []string{artist}
}

func splitTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return []string{s}
	}
	return result
}
