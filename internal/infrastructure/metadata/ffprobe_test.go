package metadata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAudioFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/music/song.mp3", true},
		{"/music/song.FLAC", true},
		{"/music/song.ogg", true},
		{"/music/song.m4a", true},
		{"/music/song.wav", true},
		{"/music/song.dsf", true},
		{"/music/song.aiff", true},
		{"/music/song.aif", true},
		{"/music/song.wma", true},
		{"/music/song.opus", true},
		{"/music/song.txt", false},
		{"/music/song.mp4", false},
		{"/music/noext", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.want, IsAudioFile(tt.path))
		})
	}
}

func TestParseFloat(t *testing.T) {
	var f float64
	parseFloat("", &f)
	assert.Equal(t, float64(0), f, "empty string leaves value untouched")

	parseFloat("245.5", &f)
	assert.Equal(t, 245.5, f)

	// Sscanf leaves the destination untouched on parse failure
	parseFloat("garbage", &f)
	assert.Equal(t, 245.5, f, "unparseable input leaves value unchanged")
}

func TestParseInt(t *testing.T) {
	var i int
	parseInt("", &i)
	assert.Equal(t, 0, i)

	parseInt("320000", &i)
	assert.Equal(t, 320000, i)

	parseInt("12/13", &i) // ffprobe track tag format "12/13"
	assert.Equal(t, 12, i, "Sscanf reads leading number")

	// Sscanf leaves the destination untouched on parse failure
	parseInt("garbage", &i)
	assert.Equal(t, 12, i, "unparseable input leaves value unchanged")
}

func TestHasCoverArt(t *testing.T) {
	assert.True(t, hasCoverArt([]ProbeStream{
		{CodecType: "video", CodecName: "mjpeg"},
	}))
	assert.False(t, hasCoverArt([]ProbeStream{
		{CodecType: "audio", CodecName: "flac"},
	}))
	assert.False(t, hasCoverArt(nil))
}

func TestSplitArtistNames(t *testing.T) {
	tests := []struct {
		name   string
		artist string
		want   []string
	}{
		{"single", "Radiohead", []string{"Radiohead"}},
		{"slash separated", "A/B", []string{"A", "B"}},
		{"comma separated", "Alice, Bob", []string{"Alice", "Bob"}},
		{"semicolon separated", "X ; Y", []string{"X", "Y"}},
		{"empty", "", nil},
		{"unknown artist", "Unknown Artist", nil},
		{"unknown artist mixed with real", "Real Artist, Unknown Artist", []string{"Real Artist"}},
		{"unknown artist mixed (case/space)", "Unknown Artist,  Real Artist", []string{"Real Artist"}},
		{"unknown artist lowercase among names", "a / unknown artist / b", []string{"a", "b"}},
		{"trims whitespace", "  A ,  B  ", []string{"A", "B"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, splitArtistNames(tt.artist))
		})
	}
}

func TestSplitTrim(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, splitTrim("a/b", "/"))
	assert.Equal(t, []string{"a", "b"}, splitTrim(" a , b ", ","))
	assert.Equal(t, []string{"a"}, splitTrim(",,,a,,,", ","))
	assert.Equal(t, []string{"///"}, splitTrim("///", "/"), "all-empty result falls back to the original string")
}

func sampleProbeResult() *ProbeResult {
	return &ProbeResult{
		Format: ProbeFormat{
			Filename:   "/music/Song.flac",
			FormatName: "flac",
			Duration:   "245.5",
			Size:       "12345678",
			BitRate:    "402780",
			Tags: map[string]string{
				"title":               "Song",
				"artist":              "Band One / Band Two",
				"album":               "Album",
				"album_artist":        "Band",
				"genre":               "Rock",
				"comment":             "notes",
				"composer":            "Comp",
				"lyricist":            "Lyr",
				"lyrics":              "[00:01.00]line",
				"arranger":            "Arr",
				"track":               "3/10",
				"disc":                "1/1",
				"date":                "1999-05-10",
				"musicbrainz_trackid": "mbid-1",
			},
		},
		Streams: []ProbeStream{
			{CodecType: "audio", CodecName: "flac", SampleRate: "44100", Channels: 2},
			{CodecType: "video", CodecName: "mjpeg"},
		},
	}
}

func TestLyricsTag(t *testing.T) {
	tests := []struct {
		name string
		tags map[string]string
		want string
	}{
		{"exact lyrics key", map[string]string{"lyrics": "[00:01.00]line"}, "[00:01.00]line"},
		{"uslt lang variant", map[string]string{"lyrics-xxx": "[00:01.00]line"}, "[00:01.00]line"},
		{"uslt zh variant", map[string]string{"lyrics-zh": "[00:01.00]中文"}, "[00:01.00]中文"},
		{"exact wins over variant", map[string]string{"lyrics": "exact", "lyrics-xxx": "variant"}, "exact"},
		{"empty variant ignored", map[string]string{"lyrics-xxx": ""}, ""},
		{"no lyrics", map[string]string{"title": "Song"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, lyricsTag(tt.tags))
		})
	}
}

func TestBuildAudioMetaFullMapping(t *testing.T) {
	meta := buildAudioMeta("/music/Song.flac", sampleProbeResult())

	assert.Equal(t, "/music/Song.flac", meta.FilePath)
	assert.Equal(t, "flac", meta.FileFormat)
	assert.Equal(t, 245.5, meta.Duration)
	assert.Equal(t, 402780, meta.BitRate)
	assert.Equal(t, int64(12345678), meta.FileSize)
	assert.Equal(t, "flac", meta.AudioCodec)
	assert.Equal(t, 44100, meta.SampleRate)
	assert.Equal(t, 2, meta.Channels)
	assert.True(t, meta.HasCoverArt)

	assert.Equal(t, "Song", meta.Title)
	assert.Equal(t, "Band One / Band Two", meta.Artist)
	assert.Equal(t, []string{"Band One", "Band Two"}, meta.Artists)
	assert.Equal(t, "Band", meta.AlbumArtist)
	assert.Equal(t, "Album", meta.Album)
	assert.Equal(t, "Rock", meta.Genre)
	assert.Equal(t, "notes", meta.Comment)
	assert.Equal(t, "Comp", meta.Composer)
	assert.Equal(t, "Lyr", meta.Lyricist)
	assert.Equal(t, "[00:01.00]line", meta.Lyrics)
	assert.Equal(t, "Arr", meta.Arranger)
	assert.Equal(t, 3, meta.TrackNumber)
	assert.Equal(t, 1, meta.DiscNumber)
	assert.Equal(t, 1999, meta.Year)
	assert.Equal(t, "mbid-1", meta.MBID)
	assert.False(t, meta.TitleFromFilename)
}

func TestBuildAudioMetaAlbumArtistFallback(t *testing.T) {
	result := sampleProbeResult()
	delete(result.Format.Tags, "album_artist")
	result.Format.Tags["albumartist"] = "Band Alt"

	meta := buildAudioMeta("/x.flac", result)
	assert.Equal(t, "Band Alt", meta.AlbumArtist)
}

func TestBuildAudioMetaMbidFallback(t *testing.T) {
	result := sampleProbeResult()
	delete(result.Format.Tags, "musicbrainz_trackid")
	result.Format.Tags["musicbrainz track id"] = "mbid-spaced"

	meta := buildAudioMeta("/x.flac", result)
	assert.Equal(t, "mbid-spaced", meta.MBID)
}

func TestBuildAudioMetaYearFallback(t *testing.T) {
	result := sampleProbeResult()
	delete(result.Format.Tags, "date")
	result.Format.Tags["year"] = "1984"

	meta := buildAudioMeta("/x.flac", result)
	assert.Equal(t, 1984, meta.Year)
}

func TestBuildAudioMetaGarbledUTF8(t *testing.T) {
	result := sampleProbeResult()
	result.Format.Tags["title"] = "Bad\xffTitle"
	result.Format.Tags["artist"] = "Bad\x80Artist"
	result.Format.Tags["album"] = "ok"

	meta := buildAudioMeta("/music/Bad Title.mp3", result)
	assert.Equal(t, "Bad Title", meta.Title, "garbled tag is cleared then falls back to filename stem")
	assert.True(t, meta.TitleFromFilename)
	assert.Equal(t, "", meta.Artist)
	assert.Equal(t, "ok", meta.Album)
}

func TestBuildAudioMetaNoTags(t *testing.T) {
	result := sampleProbeResult()
	result.Format.Tags = nil
	result.Streams = result.Streams[:1] // drop video stream

	meta := buildAudioMeta("/music/No Tags.flac", result)
	assert.Equal(t, "No Tags", meta.Title)
	assert.True(t, meta.TitleFromFilename)
	assert.Nil(t, meta.Artists)
	assert.False(t, meta.HasCoverArt)
	assert.Equal(t, 0, meta.TrackNumber)
}

func TestBuildAudioMetaEmptyResult(t *testing.T) {
	meta := buildAudioMeta("/music/x.mp3", &ProbeResult{})
	assert.Equal(t, "x", meta.Title)
	assert.True(t, meta.TitleFromFilename)
	assert.Equal(t, "", meta.FileFormat)
}

func TestProbeFailsWithoutFFprobe(t *testing.T) {
	// Probe() shells out to ffprobe; it should error cleanly when missing,
	// rather than panic.
	_, err := Probe("/nonexistent/audio.mp3")
	require.Error(t, err)
}
