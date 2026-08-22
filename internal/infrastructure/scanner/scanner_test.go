package scanner

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/infrastructure/lyrics"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

func TestTitleCase(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hello", "Hello"},
		{"Hello", "Hello"},
		{"", ""},
		{"a", "A"},
		{"héllo", "Héllo"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, titleCase(tt.in))
		})
	}
}

func TestIsYear(t *testing.T) {
	assert.True(t, isYear("1984"))
	assert.True(t, isYear("0000"))
	assert.False(t, isYear("84"))
	assert.False(t, isYear("19845"))
	assert.False(t, isYear("19a4"))
	assert.False(t, isYear(""))
}

func TestSplitByPunct(t *testing.T) {
	assert.Equal(t, []string{"Song", "Title", "Version"}, splitByPunct("Song (Title) - Version"))
	assert.Equal(t, []string{"a", "b", "c", "d"}, splitByPunct("a.b_c/d"))
	assert.Equal(t, []string{"Solo"}, splitByPunct("'Solo'"))
	assert.Equal(t, []string{"Glitter", "Green"}, splitByPunct("Glitter*Green"), "asterisk splits so the blacklist can match single fragments")
	assert.Equal(t, []string{"A", "B"}, splitByPunct("A·B"), "middle dot splits CJK artist names")
	assert.Equal(t, []string{"晴天", "Live"}, splitByPunct("晴天（Live）"), "full-width parens split CJK version markers")
	assert.Equal(t, []string{"曲名", "Acoustic"}, splitByPunct("曲名【Acoustic】"), "full-width brackets split")
	assert.Equal(t, []string{"a", "b"}, splitByPunct("a：b"), "full-width colon splits")
	assert.Empty(t, splitByPunct("---"))
	assert.Empty(t, splitByPunct(""))
}

func TestExtractFromPath(t *testing.T) {
	tests := []struct {
		name     string
		dir      string
		stem     string
		title    string
		artist   string
		album    string
		filePath string
		want     string
	}{
		{
			name:     "tags matched and removed",
			dir:      "/music/My Band/Album 2020",
			stem:     "Song (Live in Paris)",
			title:    "Song",
			artist:   "My Band",
			album:    "Album",
			filePath: "/music/My Band/Album 2020/Song (Live in Paris).flac",
			// This fallback is only reached when ExtractVersionLabel's keyword
			// scan found nothing; it joins every surviving token.
			want: "Music, Live, In, Paris",
		},
		{
			name:     "untagged filename-derived title kept out of the fallback label",
			dir:      "/music",
			stem:     "Song (Live)",
			title:    "Song (Live)",
			artist:   "",
			album:    "",
			filePath: "/music/Song (Live).flac",
			// The fallback never joins a tag-less file's whole stem into a
			// fake label: title tokens are always blacklisted here. Version
			// detection for untagged files lives in ExtractVersionLabel's
			// keyword pass, not in this fallback.
			want: "Music",
		},
		{
			name:     "version keyword in album/artist is blacklisted",
			dir:      "/music",
			stem:     "Song",
			title:    "Song",
			artist:   "Live",
			album:    "Deluxe Edition",
			filePath: "/music/Song.flac",
			want:     "Music", // "live" belongs to the artist, not a version marker
		},
		{
			name:     "year dropped",
			dir:      "/music",
			stem:     "Track 1999",
			title:    "Track",
			artist:   "Artist",
			album:    "Album",
			filePath: "/music/Track 1999.mp3",
			want:     "Music",
		},
		{
			name:     "title tokens all blacklisted, dir word remains",
			dir:      "/music",
			stem:     "Same",
			title:    "Same",
			artist:   "",
			album:    "",
			filePath: "/music/Same.mp3",
			want:     "Music",
		},
		{
			name:     "album words blacklisted",
			dir:      "/music",
			stem:     "Intro (Greatest Hits)",
			title:    "Intro",
			artist:   "",
			album:    "Greatest Hits",
			filePath: "/music/Intro (Greatest Hits).flac",
			want:     "Music", // title/album/artist words dropped, dir word survives
		},
		{
			name:     "comma-separated multi-artist fully blacklisted",
			dir:      "司夏,河图",
			stem:     "缘生意转 - 司夏,河图",
			title:    "缘生意转",
			artist:   "司夏,河图",
			album:    "墨缘记·洇",
			filePath: "/music/司夏,河图/缘生意转 - 司夏,河图.mp3",
			want:     "", // every path token is a known field; no label survives
		},
		{
			name:     "track number and disc markers dropped",
			dir:      "/music/Disc 1",
			stem:     "01 - Song Title",
			title:    "Song Title",
			artist:   "",
			album:    "",
			filePath: "/music/Disc 1/01 - Song Title.flac",
			want:     "Music", // "01", "disc" dropped; the dir word survives like other tests
		},
		{
			name:     "track number alone must not leak into a label",
			dir:      "/music",
			stem:     "03",
			title:    "03",
			artist:   "Artist",
			album:    "Album",
			filePath: "/music/03.flac",
			want:     "Music",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFromPath(tt.dir, tt.stem, tt.title, tt.artist, tt.album, tt.filePath)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHashFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	require.NoError(t, os.WriteFile(path, []byte("hello world"), 0644))

	h1, err := hashFile(path)
	require.NoError(t, err)
	assert.Len(t, h1, 64, "sha256 hex")

	h2, err := hashFile(path)
	require.NoError(t, err)
	assert.Equal(t, h1, h2, "same content same hash")

	require.NoError(t, os.WriteFile(path, []byte("changed"), 0644))
	h3, err := hashFile(path)
	require.NoError(t, err)
	assert.NotEqual(t, h1, h3, "different content different hash")
}

func TestHashFileLargeContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.bin")
	data := make([]byte, 1<<20)
	_, err := rand.Read(data)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0644))

	h, err := hashFile(path)
	require.NoError(t, err)
	assert.Len(t, h, 64)
}

func TestHashFileMissing(t *testing.T) {
	_, err := hashFile("/nonexistent/file.mp3")
	require.Error(t, err)
}

func TestTimePtr(t *testing.T) {
	now := time.Now()
	p := timePtr(now)
	require.NotNil(t, p)
	assert.Equal(t, now, *p)
}

func TestMergeKey(t *testing.T) {
	key, ok := mergeKey("缘生意转 (Live)", "漱愿记·漱", "司夏\x1f河图")
	require.True(t, ok)

	key2, ok2 := mergeKey("缘生意转", "漱愿记·漱", "河图\x1f司夏")
	require.True(t, ok2)
	assert.Equal(t, key, key2, "paren suffix stripped and artist order normalized")

	// A single resolved artist name containing "/" stays whole: it must not
	// be split into two names, which would falsely merge a one-artist track
	// with a track that genuinely has those two artists.
	bandKey, okBand := mergeKey("Song", "Album", "AC/DC")
	require.True(t, okBand)
	splitKey, okSplit := mergeKey("Song", "Album", "AC\x1fDC")
	require.True(t, okSplit)
	assert.NotEqual(t, bandKey, splitKey, "AC/DC (one artist) never matches AC + DC (two artists)")

	// An artist name containing a comma is a single resolved name: splitting
	// it on commas would falsely collide with two separate artists.
	commaBand, okCB := mergeKey("Song", "Album", "Earth, Wind & Fire")
	require.True(t, okCB)
	twoArtists, okTA := mergeKey("Song", "Album", "Earth\x1fWind & Fire")
	require.True(t, okTA)
	assert.NotEqual(t, commaBand, twoArtists, "\"Earth, Wind & Fire\" (one artist) never matches Earth + Wind & Fire (two)")

	// Albums arrive \x1f-separated; order is irrelevant.
	alKey, okAl := mergeKey("Song", "A\x1fB", "Artist")
	require.True(t, okAl)
	alKey2, okAl2 := mergeKey("Song", "B\x1fA", "Artist")
	require.True(t, okAl2)
	assert.Equal(t, alKey, alKey2, "album order normalized")

	// An album title containing "|" stays whole and cannot collide with the
	// \x1f-joined form of two separate albums.
	pipeKey, okPipe := mergeKey("Song", "X|Y", "Artist")
	require.True(t, okPipe)
	twoAlbumKey, okTwo := mergeKey("Song", "X\x1fY", "Artist")
	require.True(t, okTwo)
	assert.NotEqual(t, pipeKey, twoAlbumKey, "one album \"X|Y\" never matches two albums X and Y")

	_, ok = mergeKey("", "Album", "Artist")
	assert.False(t, ok, "empty title disqualified")
	_, ok = mergeKey("Song", "", "Artist")
	assert.False(t, ok, "empty album disqualified")
	_, ok = mergeKey("Song", "Album", "")
	assert.False(t, ok, "empty artist disqualified")
	_, ok = mergeKey("Song", "Unknown Album", "Artist")
	assert.False(t, ok, "unknown album disqualified")
	_, ok = mergeKey("Song", "Album", "Unknown Artist")
	assert.False(t, ok, "unknown artist disqualified")
	_, ok = mergeKey("Song", "Album", "Unknown Artist\x1fJohn Doe")
	assert.False(t, ok, "unknown component among multi-artist disqualified")
	_, ok = mergeKey("Song", "Unknown Album\x1fGreatest Hits", "Artist")
	assert.False(t, ok, "unknown album component among multi-album disqualified")
}

func TestSourcePriority(t *testing.T) {
	e := &Engine{}
	e.sourcePrio = registrySourcePriorities(metadata.NewRegistry(
		&fakePrioSource{name: "musicbrainz", enabled: true, priority: 10},
		&fakePrioSource{name: "netease", enabled: true, priority: 20},
	))
	// Registry default ordering: musicbrainz before netease.
	assert.Equal(t, 10, e.sourcePriority("musicbrainz"))
	assert.Equal(t, 20, e.sourcePriority("netease"))
	// Unknown / disabled sources rank last.
	assert.Equal(t, 100, e.sourcePriority("qq"))
	assert.Equal(t, 100, e.sourcePriority(""))

	// Raising NetEase above MusicBrainz must change the main-source winner.
	e.sourcePrio = registrySourcePriorities(metadata.NewRegistry(
		&fakePrioSource{name: "musicbrainz", enabled: true, priority: 10},
		&fakePrioSource{name: "netease", enabled: true, priority: 5},
	))
	assert.Less(t, e.sourcePriority("netease"), e.sourcePriority("musicbrainz"))

	// Disabled sources are dropped from the map (fall back to 100).
	e.sourcePrio = registrySourcePriorities(metadata.NewRegistry(
		&fakePrioSource{name: "netease", enabled: false, priority: 5},
	))
	assert.Equal(t, 100, e.sourcePriority("netease"))
}

func TestAlignGroupMainSourceFollowsRegistryPriority(t *testing.T) {
	e := &Engine{sourcePrio: registrySourcePriorities(metadata.NewRegistry(
		&fakePrioSource{name: "musicbrainz", enabled: true, priority: 10},
		&fakePrioSource{name: "netease", enabled: true, priority: 5},
	))}
	mainSource, mainID := alignGroupMain(e, []mergeCandidate{
		{mt: repository.MergeTrack{ID: "t1", MetadataSource: "netease", ExternalID: "ne-1"}},
		{mt: repository.MergeTrack{ID: "t2", MetadataSource: "musicbrainz", ExternalID: "mb-1"}},
	})
	assert.Equal(t, "netease", mainSource)
	assert.Equal(t, "ne-1", mainID)

	// Default ordering keeps musicbrainz first.
	e.sourcePrio = registrySourcePriorities(metadata.NewRegistry(
		&fakePrioSource{name: "musicbrainz", enabled: true, priority: 10},
		&fakePrioSource{name: "netease", enabled: true, priority: 20},
	))
	mainSource, mainID = alignGroupMain(e, []mergeCandidate{
		{mt: repository.MergeTrack{ID: "t1", MetadataSource: "netease", ExternalID: "ne-1"}},
		{mt: repository.MergeTrack{ID: "t2", MetadataSource: "musicbrainz", ExternalID: "mb-1"}},
	})
	assert.Equal(t, "musicbrainz", mainSource)
	assert.Equal(t, "mb-1", mainID)

	// Members without any external id never become main.
	e.sourcePrio = registrySourcePriorities(metadata.NewRegistry(
		&fakePrioSource{name: "musicbrainz", enabled: true, priority: 10},
	))
	mainSource, mainID = alignGroupMain(e, []mergeCandidate{
		{mt: repository.MergeTrack{ID: "t1", MetadataSource: "musicbrainz", ExternalID: ""}},
		{mt: repository.MergeTrack{ID: "t2", MetadataSource: "netease", ExternalID: ""}},
	})
	assert.Equal(t, "", mainSource)
	assert.Equal(t, "", mainID)
}

// registrySourcePriorities mirrors NewEngine's map build from a registry.
func registrySourcePriorities(r *metadata.Registry) map[string]int {
	m := make(map[string]int)
	for _, s := range r.Sources() {
		if s != nil {
			m[s.Name()] = s.Priority()
		}
	}
	return m
}

// alignGroupMain extracts alignGroup's main-source selection so the priority
// coupling can be asserted without a database.
func alignGroupMain(e *Engine, members []mergeCandidate) (string, string) {
	mainSource := ""
	mainID := ""
	best := 100
	for _, c := range members {
		if c.mt.ExternalID == "" {
			continue
		}
		prio := e.sourcePriority(c.mt.MetadataSource)
		if prio < best {
			best = prio
			mainSource = c.mt.MetadataSource
			mainID = c.mt.ExternalID
		}
	}
	return mainSource, mainID
}

type fakePrioSource struct {
	name     string
	enabled  bool
	priority int
}

func (f *fakePrioSource) Name() string                      { return f.name }
func (f *fakePrioSource) Label() string                     { return f.name }
func (f *fakePrioSource) Enabled() bool                     { return f.enabled }
func (f *fakePrioSource) Priority() int                     { return f.priority }
func (f *fakePrioSource) Capabilities() port.MetadataFields { return 0 }
func (f *fakePrioSource) SearchCandidates(context.Context, port.MetadataQuery) ([]port.MetadataCandidate, error) {
	return nil, nil
}
func (f *fakePrioSource) Identify(context.Context, port.MetadataQuery) (*port.MetadataCandidate, error) {
	return nil, nil
}
func (f *fakePrioSource) Lookup(context.Context, string) (*port.MetadataCandidate, error) {
	return nil, nil
}
func (f *fakePrioSource) SearchArtists(context.Context, string) ([]port.ArtistSearchResult, error) {
	return nil, nil
}
func (f *fakePrioSource) SearchReleases(context.Context, string) ([]port.ReleaseSearchResult, error) {
	return nil, nil
}
func (f *fakePrioSource) LookupAlbum(context.Context, string) (*port.AlbumDetail, error) {
	return nil, nil
}
func (f *fakePrioSource) LookupArtist(context.Context, string) (*port.ArtistLookupDetail, error) {
	return nil, nil
}

func TestMergeExternalIDs(t *testing.T) {
	got := mergeExternalIDs(
		map[string]string{"musicbrainz": "mb-1"},
		map[string]string{"netease": "ne-1", "musicbrainz": ""},
	)
	assert.Equal(t, map[string]string{"musicbrainz": "mb-1", "netease": "ne-1"}, got)
	assert.Equal(t, map[string]string{}, mergeExternalIDs(nil, nil))
}

func TestSweepOrphanLyrics(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	// Program-generated track ids are 26 hex chars (domain.NewID).
	validID := "1786768654472f9c6c395f0672"
	orphanID := "1786768654472f9c6c395f0673"
	lyricsDir := t.TempDir()
	libDir := filepath.Join(lyricsDir, "lib-1")
	require.NoError(t, os.MkdirAll(libDir, 0755))
	validPath := filepath.Join(libDir, validID+"_p0.lrc")
	orphanPath := filepath.Join(libDir, orphanID+"_p2.lrc")
	plainPath := filepath.Join(libDir, "nota-lyrics.txt")
	fakeLikePath := filepath.Join(libDir, "my_song_playlist_p9.lrc")
	for _, p := range []string{validPath, orphanPath, plainPath, fakeLikePath} {
		require.NoError(t, os.WriteFile(p, []byte("line"), 0644))
	}

	// The sweep batch-loads the library's existing track ids once, then
	// removes files whose track row is absent.
	checkQ := regexp.QuoteMeta(`SELECT id FROM tracks WHERE library_id = $1`)
	mock.ExpectQuery(checkQ).WithArgs("lib-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(validID))

	e := &Engine{db: db, lyricsStore: lyrics.NewStore(lyricsDir)}
	e.sweepOrphanLyrics(context.Background(), "lib-1")

	_, err = os.Stat(orphanPath)
	assert.True(t, os.IsNotExist(err), "orphan lyrics removed")
	_, err = os.Stat(validPath)
	assert.NoError(t, err, "valid lyrics kept")
	_, err = os.Stat(plainPath)
	assert.NoError(t, err, "non-lyrics file untouched")
	_, err = os.Stat(fakeLikePath)
	assert.NoError(t, err, "hand-made file resembling the layout is never deleted")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestSyncVersionGroupsMainVersionUnion: the main version's write-back must
// carry the group-wide id union (its own aliases plus every member's source
// ids), not just its own map — otherwise alias lookups against the main
// version via external_ids @> miss ids only the secondary members carry.
func TestSyncVersionGroupsMainVersionUnion(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	e := &Engine{db: db, trackRepo: repository.NewTrackRepo(db)}

	// All group members load in one query with the group key and current
	// identity; no per-member version lookup or FindByID.
	mock.ExpectQuery(regexp.QuoteMeta("FROM track_version_groups g JOIN tracks t ON t.id = g.track_id")).
		WithArgs("lib-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"track_id", "version", "g.metadata_source", "g.external_id",
			"t.metadata_source", "t.external_id", "t.external_ids",
		}).
			AddRow("m-1", 1, "musicbrainz", "mbid-main", "musicbrainz", "mbid-main", `{"musicbrainz":"mbid-main"}`).
			AddRow("m-2", 2, "musicbrainz", "mbid-main", "musicbrainz", "mbid-main", `{"musicbrainz":"mbid-main","netease":"ne-1"}`))

	// The secondary already carries the union → no member write; the main
	// version must be written with ids ∪ groupExternalIDs.
	union := []byte(`{"musicbrainz":"mbid-main","netease":"ne-1"}`)
	mock.ExpectExec(regexp.QuoteMeta("UPDATE tracks SET metadata_source = $1, external_id = $2, external_ids = $3, updated_at = NOW() WHERE id = $4")).
		WithArgs("musicbrainz", "mbid-main", union, "m-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	e.syncVersionGroups(context.Background(), "lib-1")
	require.NoError(t, mock.ExpectationsWereMet())
}
