package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lib/pq"
	"github.com/sonicore/server/internal/core/domain"
)

type TrackRepo struct {
	db *sql.DB
}

func NewTrackRepo(db *sql.DB) *TrackRepo {
	return &TrackRepo{db: db}
}

// trackScanDests returns the scan destinations for the canonical track
// columns. Shared by scanTrack and scanTrackWithExtID so the column order
// cannot drift between the two.
func trackScanDests(t *domain.Track, metadata *sql.NullString, coverID *sql.NullString, extIDs *[]byte) []interface{} {
	return []interface{}{
		&t.ID, &t.LibraryID, &t.Title,
		coverID,
		&t.Duration, &t.BitRate, &t.SampleRate,
		&t.Channels, &t.FilePath, &t.FileSize, &t.FileFormat, &t.AudioCodec, &t.ExternalID, &t.MetadataSource, extIDs, &t.AcoustID,
		&t.Hash, &t.LyricsMask, &t.LyricsOffset, &t.Heat, &t.PlayCount, &t.LastPlayedAt,
		metadata, &t.Version, &t.VersionLabel, &t.CreatedAt, &t.UpdatedAt,
	}
}

// parseExternalIDs decodes a stored external_ids jsonb value into dst,
// treating empty markers ("{}", "null") as "not present". Shared by the track
// scan paths and LoadMergeCandidates so the JSON handling cannot drift.
func parseExternalIDs(extIDs []byte, dst *map[string]string) error {
	if len(extIDs) == 0 || string(extIDs) == "{}" || string(extIDs) == "null" {
		return nil
	}
	if err := json.Unmarshal(extIDs, dst); err != nil {
		return fmt.Errorf("parse external_ids: %w", err)
	}
	return nil
}

// finishTrackScan applies the post-scan decoding shared by scanTrack and
// scanTrackWithExtID.
func finishTrackScan(t *domain.Track, metadata, coverID sql.NullString, extIDs []byte) error {
	if coverID.Valid {
		t.CoverImageID = &coverID.String
	}
	return parseExternalIDs(extIDs, &t.ExternalIDs)
}

func scanTrack(scanner interface{ Scan(dest ...interface{}) error }) (*domain.Track, error) {
	var t domain.Track
	var metadata sql.NullString
	var coverID sql.NullString
	var extIDs []byte
	if err := scanner.Scan(trackScanDests(&t, &metadata, &coverID, &extIDs)...); err != nil {
		return nil, err
	}
	if err := finishTrackScan(&t, metadata, coverID, extIDs); err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *TrackRepo) BatchCreate(ctx context.Context, tracks []domain.Track) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO tracks (id, library_id, title,
		 cover_image_id,
		 duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, audio_codec, external_id, metadata_source, external_ids, acoust_id, hash,
		 lyrics_mask, lyrics_offset, heat, play_count, metadata, version, version_label, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	taStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO track_artists (track_id, artist_id, role, sort_order)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (track_id, artist_id, role) DO NOTHING`)
	if err != nil {
		return err
	}
	defer taStmt.Close()

	talStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO track_albums (track_id, album_id, track_number, disc_number)
		 VALUES ($1,$2,$3,$4)
		 ON CONFLICT (track_id, album_id) DO NOTHING`)
	if err != nil {
		return err
	}
	defer talStmt.Close()

	for _, t := range tracks {
		ext, err := externalIDsArg(t.ExternalIDs)
		if err != nil {
			return err
		}
		_, err = stmt.ExecContext(ctx, t.ID, t.LibraryID, t.Title,
			t.CoverImageID,
			t.Duration, t.BitRate, t.SampleRate, t.Channels,
			t.FilePath, t.FileSize, t.FileFormat, t.AudioCodec, t.ExternalID, sourceOrDefault(t.MetadataSource), ext, t.AcoustID, t.Hash,
			t.LyricsMask, t.LyricsOffset, t.Heat, t.PlayCount, t.Metadata, t.Version, t.VersionLabel, t.CreatedAt, t.UpdatedAt)
		if err != nil {
			return err
		}
		for i, ta := range t.Artists {
			sortOrder := ta.SortOrder
			if sortOrder == 0 {
				sortOrder = i
			}
			_, err = taStmt.ExecContext(ctx, t.ID, ta.ArtistID, ta.Role, sortOrder)
			if err != nil {
				return err
			}
		}
		for _, tal := range t.Albums {
			_, err = talStmt.ExecContext(ctx, t.ID, tal.AlbumID, tal.TrackNumber, tal.DiscNumber)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func (r *TrackRepo) LoadTrackAlbums(ctx context.Context, trackID string) ([]*domain.TrackAlbum, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT ta.track_id, ta.album_id, ta.track_number, ta.disc_number,
		 al.title, al.cover_image_id
		 FROM track_albums ta
		 JOIN albums al ON al.id = ta.album_id
		 WHERE ta.track_id = $1
		 ORDER BY ta.disc_number, ta.track_number`, trackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var albums []*domain.TrackAlbum
	for rows.Next() {
		var tal domain.TrackAlbum
		var album domain.Album
		if err := rows.Scan(&tal.TrackID, &tal.AlbumID, &tal.TrackNumber, &tal.DiscNumber,
			&album.Title, &album.CoverImageID); err != nil {
			return nil, err
		}
		tal.Album = &album
		albums = append(albums, &tal)
	}
	return albums, rows.Err()
}

func (r *TrackRepo) LoadTrackAlbumsBulk(ctx context.Context, trackIDs []string) (map[string][]*domain.TrackAlbum, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT ta.track_id, ta.album_id, ta.track_number, ta.disc_number,
		 al.title, al.cover_image_id
		 FROM track_albums ta
		 JOIN albums al ON al.id = ta.album_id
		 WHERE ta.track_id = ANY($1)
		 ORDER BY ta.track_id, ta.disc_number, ta.track_number`, pq.Array(trackIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]*domain.TrackAlbum)
	for rows.Next() {
		var tal domain.TrackAlbum
		var album domain.Album
		if err := rows.Scan(&tal.TrackID, &tal.AlbumID, &tal.TrackNumber, &tal.DiscNumber,
			&album.Title, &album.CoverImageID); err != nil {
			return nil, err
		}
		tal.Album = &album
		result[tal.TrackID] = append(result[tal.TrackID], &tal)
	}
	return result, rows.Err()
}

func (r *TrackRepo) FindByID(ctx context.Context, id string) (*domain.Track, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, title,
		 cover_image_id,
		 duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, audio_codec, external_id, metadata_source, external_ids, acoust_id, hash,
		 lyrics_mask, lyrics_offset, heat, play_count, last_played_at, metadata, version, version_label, created_at, updated_at
		 FROM tracks WHERE id = $1`, id)
	t, err := scanTrack(row)
	if err != nil {
		return nil, err
	}
	t.Albums, err = r.LoadTrackAlbums(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	t.Artists, err = r.LoadTrackArtists(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	if len(t.Artists) > 0 {
		t.Artist = t.Artists[0].Artist
	}
	return t, nil
}

func (r *TrackRepo) FindByIDs(ctx context.Context, ids []string) ([]*domain.Track, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, title,
		 cover_image_id,
		 duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, audio_codec, external_id, metadata_source, external_ids, acoust_id, hash,
		 lyrics_mask, lyrics_offset, heat, play_count, last_played_at, metadata, version, version_label, created_at, updated_at
		 FROM tracks WHERE id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*domain.Track
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Bulk-load track_albums and track_artists
	trackAlbums, err := r.LoadTrackAlbumsBulk(ctx, ids)
	if err != nil {
		return nil, err
	}
	trackArtists, err := r.LoadTrackArtistsBulk(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, t := range list {
		t.Albums = trackAlbums[t.ID]
		t.Artists = trackArtists[t.ID]
		if len(t.Artists) > 0 {
			t.Artist = t.Artists[0].Artist
		}
	}
	return list, nil
}

type trackArtistRow struct {
	TrackID        string
	ArtistID       string
	Role           string
	SortOrder      int
	Name           string
	ExternalID     string
	MetadataSource string
}

func (r *TrackRepo) LoadTrackArtists(ctx context.Context, trackID string) ([]*domain.TrackArtist, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT ta.track_id, ta.artist_id, ta.role, ta.sort_order, a.name, a.external_id, a.metadata_source
		 FROM track_artists ta
		 JOIN artists a ON a.id = ta.artist_id
		 WHERE ta.track_id = $1
		 ORDER BY ta.sort_order`, trackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artists []*domain.TrackArtist
	for rows.Next() {
		var row trackArtistRow
		if err := rows.Scan(&row.TrackID, &row.ArtistID, &row.Role, &row.SortOrder, &row.Name, &row.ExternalID, &row.MetadataSource); err != nil {
			return nil, err
		}
		artists = append(artists, &domain.TrackArtist{
			TrackID:   row.TrackID,
			ArtistID:  row.ArtistID,
			Role:      row.Role,
			SortOrder: row.SortOrder,
			Artist:    &domain.Artist{ID: row.ArtistID, Name: row.Name, ExternalID: row.ExternalID, MetadataSource: row.MetadataSource},
		})
	}
	return artists, rows.Err()
}

func (r *TrackRepo) LoadTrackArtistsBulk(ctx context.Context, trackIDs []string) (map[string][]*domain.TrackArtist, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT ta.track_id, ta.artist_id, ta.role, ta.sort_order, a.name, a.external_id, a.metadata_source
		 FROM track_artists ta
		 JOIN artists a ON a.id = ta.artist_id
		 WHERE ta.track_id = ANY($1)
		 ORDER BY ta.track_id, ta.sort_order`, pq.Array(trackIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]*domain.TrackArtist)
	for rows.Next() {
		var row trackArtistRow
		if err := rows.Scan(&row.TrackID, &row.ArtistID, &row.Role, &row.SortOrder, &row.Name, &row.ExternalID, &row.MetadataSource); err != nil {
			return nil, err
		}
		result[row.TrackID] = append(result[row.TrackID], &domain.TrackArtist{
			TrackID:   row.TrackID,
			ArtistID:  row.ArtistID,
			Role:      row.Role,
			SortOrder: row.SortOrder,
			Artist:    &domain.Artist{ID: row.ArtistID, Name: row.Name, ExternalID: row.ExternalID, MetadataSource: row.MetadataSource},
		})
	}
	return result, rows.Err()
}

func (r *TrackRepo) FindByLibraryID(ctx context.Context, libraryIDs ...string) ([]domain.Track, error) {
	if len(libraryIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, title,
		 cover_image_id,
		 duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, audio_codec, external_id, metadata_source, external_ids, acoust_id, hash,
		 lyrics_mask, lyrics_offset, heat, play_count, last_played_at, metadata, version, version_label, created_at, updated_at
		 FROM tracks WHERE library_id = ANY($1)`, pq.Array(libraryIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []domain.Track
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, *t)
	}
	return tracks, rows.Err()
}

func (r *TrackRepo) FindByAlbumID(ctx context.Context, albumID string) ([]domain.Track, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.id, t.library_id, t.title,
		 t.cover_image_id,
		 t.duration, t.bit_rate, t.sample_rate, t.channels,
		 t.file_path, t.file_size, t.file_format, t.audio_codec, t.external_id, t.metadata_source, t.external_ids, t.acoust_id, t.hash,
		 t.lyrics_mask, t.lyrics_offset, t.heat, t.play_count, t.last_played_at, t.metadata, t.version, t.version_label, t.created_at, t.updated_at
		 FROM tracks t
		 INNER JOIN track_albums ta ON ta.track_id = t.id
		 WHERE ta.album_id = $1
		 ORDER BY ta.disc_number, ta.track_number`, albumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []domain.Track
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, *t)
	}
	return tracks, rows.Err()
}

func (r *TrackRepo) FindByArtistID(ctx context.Context, artistID string) ([]domain.Track, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.id, t.library_id, t.title,
		 t.cover_image_id,
		 t.duration, t.bit_rate, t.sample_rate, t.channels,
		 t.file_path, t.file_size, t.file_format, t.audio_codec, t.external_id, t.metadata_source, t.external_ids, t.acoust_id, t.hash,
		 t.lyrics_mask, t.lyrics_offset, t.heat, t.play_count, t.last_played_at, t.metadata, t.version, t.version_label, t.created_at, t.updated_at
		 FROM tracks t
		 INNER JOIN track_artists ta ON ta.track_id = t.id
		 WHERE ta.artist_id = $1`, artistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var trackIDs []string
	var tracks []domain.Track
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		trackIDs = append(trackIDs, t.ID)
		tracks = append(tracks, *t)
	}

	if len(trackIDs) > 0 {
		tas, _ := r.LoadTrackArtistsBulk(ctx, trackIDs)
		albums, _ := r.LoadTrackAlbumsBulk(ctx, trackIDs)
		for i := range tracks {
			tracks[i].Artists = tas[tracks[i].ID]
			tracks[i].Albums = albums[tracks[i].ID]
		}
	}
	return tracks, rows.Err()
}

func (r *TrackRepo) FindByHash(ctx context.Context, hash string) (*domain.Track, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, title,
		 cover_image_id,
		 duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, audio_codec, external_id, metadata_source, external_ids, acoust_id, hash,
		 lyrics_mask, lyrics_offset, heat, play_count, last_played_at, metadata, version, version_label, created_at, updated_at
		 FROM tracks WHERE hash = $1`, hash)
	return scanTrack(row)
}

func (r *TrackRepo) Update(ctx context.Context, track *domain.Track) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	ext, err := externalIDsArg(track.ExternalIDs)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`UPDATE tracks SET title=$1, cover_image_id=$2,
		 duration=$3, bit_rate=$4, sample_rate=$5, channels=$6,
		 file_path=$7, file_size=$8, file_format=$9, audio_codec=$10, external_id=$11, metadata_source=$12, external_ids=$13, acoust_id=$14,
		 hash=$15, lyrics_mask=$16, lyrics_offset=$17, heat=$18, play_count=$19,
		 last_played_at=$20, metadata=$21, version=$22, version_label=$23, updated_at=NOW()
		 WHERE id=$24`,
		track.Title, track.CoverImageID,
		track.Duration, track.BitRate, track.SampleRate, track.Channels,
		track.FilePath, track.FileSize, track.FileFormat, track.AudioCodec, track.ExternalID, sourceOrDefault(track.MetadataSource), ext, track.AcoustID,
		track.Hash, track.LyricsMask, track.LyricsOffset, track.Heat, track.PlayCount,
		track.LastPlayedAt, track.Metadata, track.Version, track.VersionLabel, track.ID)
	if err != nil {
		return err
	}

	// Replace track_albums
	if len(track.Albums) > 0 {
		_, err = tx.ExecContext(ctx, `DELETE FROM track_albums WHERE track_id = $1`, track.ID)
		if err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO track_albums (track_id, album_id, track_number, disc_number)
			 VALUES ($1,$2,$3,$4)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, tal := range track.Albums {
			_, err = stmt.ExecContext(ctx, track.ID, tal.AlbumID, tal.TrackNumber, tal.DiscNumber)
			if err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// SetCoverImage points a track at its images row without touching related
// tables. Safe to call before the track row exists (no-op, the create path
// persists the in-memory value) and from the on-demand cover restore path.
func (r *TrackRepo) SetCoverImage(ctx context.Context, trackID, imageID string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tracks SET cover_image_id=$1, updated_at=NOW() WHERE id=$2`,
		imageID, trackID)
	return err
}

func (r *TrackRepo) UpdateLyricsOffset(ctx context.Context, trackID string, offset float64) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tracks SET lyrics_offset=$1, updated_at=NOW() WHERE id=$2`,
		offset, trackID)
	return err
}

func (r *TrackRepo) DeleteByFilePath(ctx context.Context, path, libraryID string) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx,
		`DELETE FROM tracks WHERE file_path = $1 AND library_id = $2 RETURNING id`, path, libraryID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// FindIDByFilePath resolves the track id for a path without deleting the
// row (used to gate cover cleanup before the track row is removed).
func (r *TrackRepo) FindIDByFilePath(ctx context.Context, path, libraryID string) (string, error) {
	var id string
	err := r.db.QueryRowContext(ctx,
		`SELECT id FROM tracks WHERE file_path = $1 AND library_id = $2`, path, libraryID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

// DeleteByID removes a track row by id.
func (r *TrackRepo) DeleteByID(ctx context.Context, trackID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM tracks WHERE id = $1`, trackID)
	return err
}

func (r *TrackRepo) ReplaceTrackArtists(ctx context.Context, trackID string, artists []*domain.TrackArtist) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `DELETE FROM track_artists WHERE track_id = $1`, trackID)
	if err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO track_artists (track_id, artist_id, role, sort_order)
		 VALUES ($1,$2,$3,$4)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, ta := range artists {
		sortOrder := ta.SortOrder
		if sortOrder == 0 {
			sortOrder = i
		}
		_, err = stmt.ExecContext(ctx, trackID, ta.ArtistID, ta.Role, sortOrder)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *TrackRepo) ReplaceTrackAlbums(ctx context.Context, trackID string, albums []*domain.TrackAlbum) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `DELETE FROM track_albums WHERE track_id = $1`, trackID)
	if err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO track_albums (track_id, album_id, track_number, disc_number)
		 VALUES ($1,$2,$3,$4)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, tal := range albums {
		_, err = stmt.ExecContext(ctx, trackID, tal.AlbumID, tal.TrackNumber, tal.DiscNumber)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// FindVersionsByExternalID returns the sibling versions sharing the given
// (metadataSource, externalID) identity. The source is required: different
// sources can legally reuse the same external_id, so a bare external_id
// lookup would pull in unrelated groups. Membership is derived from the
// tracks table's current (metadata_source, external_id) rather than the
// track_version_groups mirror, so a scan that has just aligned a member to a
// new group key is visible immediately (no two-table sync window).
func (r *TrackRepo) FindVersionsByExternalID(ctx context.Context, metadataSource, externalID string, excludeTrackID string, accessibleLibIDs []string) ([]domain.Track, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.id, t.library_id, t.title,
		 t.cover_image_id,
		 t.duration, t.bit_rate, t.sample_rate, t.channels,
		 t.file_path, t.file_size, t.file_format, t.audio_codec, t.external_id, t.metadata_source, t.external_ids, t.acoust_id, t.hash,
		 t.lyrics_mask, t.lyrics_offset, t.heat, t.play_count, t.last_played_at, t.metadata, t.version, t.version_label, t.created_at, t.updated_at
		 FROM tracks t
		 WHERE t.metadata_source = $1 AND t.external_id = $2 AND t.id != $3 AND t.library_id = ANY($4) AND t.version >= 1
		 ORDER BY t.version`, metadataSource, externalID, excludeTrackID, pq.Array(accessibleLibIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tracks []domain.Track
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, *t)
	}
	return tracks, rows.Err()
}

// VersionGroupKey identifies a version group by its (metadata_source,
// external_id) identity. Two sources can legitimately use the same
// external_id for different works, so the key must carry the source — a bare
// external_id key would silently merge unrelated groups.
type VersionGroupKey struct {
	MetadataSource string
	ExternalID     string
}

// FindVersionsByExternalIDBulk loads every track whose current identity
// matches one of the requested (metadata_source, external_id) pairs, grouped
// by that same pair. Group membership is derived from the tracks table
// directly (not the track_version_groups mirror), so a scan that has just
// aligned a member to a new group key is visible immediately. Only grouped
// tracks (version >= 1) participate; version-0 singletons are excluded so a
// lone track sharing an id in another library never shows up as a "version".
func (r *TrackRepo) FindVersionsByExternalIDBulk(ctx context.Context, keys []VersionGroupKey, accessibleLibIDs []string) (map[VersionGroupKey][]domain.Track, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	// Two parallel arrays feed the row-constructor match below; the source
	// dimension is pushed down so rows carrying the same external_id under a
	// different source are not shipped back just to be discarded in memory.
	sources := make([]string, len(keys))
	extIDs := make([]string, len(keys))
	for i, k := range keys {
		sources[i] = k.MetadataSource
		extIDs[i] = k.ExternalID
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.external_id, t.id, t.library_id, t.title,
		 t.cover_image_id,
		 t.duration, t.bit_rate, t.sample_rate, t.channels,
		 t.file_path, t.file_size, t.file_format, t.audio_codec, t.external_id, t.metadata_source, t.external_ids, t.acoust_id, t.hash,
		 t.lyrics_mask, t.lyrics_offset, t.heat, t.play_count, t.last_played_at, t.metadata, t.version, t.version_label, t.created_at, t.updated_at
		 FROM tracks t
		 WHERE EXISTS (
		   SELECT 1 FROM unnest($1::text[], $2::text[]) AS u(source, ext_id)
		   WHERE u.source = t.metadata_source AND u.ext_id = t.external_id
		 )
		 AND t.library_id = ANY($3) AND t.version >= 1
		 ORDER BY t.metadata_source, t.external_id, t.version`, pq.Array(sources), pq.Array(extIDs), pq.Array(accessibleLibIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[VersionGroupKey][]domain.Track)
	for rows.Next() {
		var groupExtID string
		t, err := scanTrackWithExtID(rows, &groupExtID)
		if err != nil {
			return nil, err
		}
		key := VersionGroupKey{MetadataSource: t.MetadataSource, ExternalID: groupExtID}
		result[key] = append(result[key], *t)
	}
	return result, rows.Err()
}

// scanTrackWithExtID scans a track row whose first column is a version-group
// external id, then the canonical track columns — all in a single Scan so the
// row is consumed exactly once.
func scanTrackWithExtID(scanner interface{ Scan(dest ...interface{}) error }, groupExtID *string) (*domain.Track, error) {
	var t domain.Track
	var metadata sql.NullString
	var coverID sql.NullString
	var extIDs []byte
	dests := trackScanDests(&t, &metadata, &coverID, &extIDs)
	all := make([]interface{}, 0, len(dests)+1)
	all = append(all, groupExtID)
	all = append(all, dests...)
	if err := scanner.Scan(all...); err != nil {
		return nil, err
	}
	if err := finishTrackScan(&t, metadata, coverID, extIDs); err != nil {
		return nil, err
	}
	return &t, nil
}

// MergeTrack is a lightweight track projection for the duplicate-merge pass.
type MergeTrack struct {
	ID             string
	Title          string
	Artists        string // \x1f-separated, ordered (unit separator cannot appear in names)
	Albums         string // \x1f-separated, ordered (unit separator cannot appear in titles)
	MetadataSource string
	ExternalID     string
	ExternalIDs    map[string]string
	Version        int
}

// FindByExternalID locates the track in the library whose primary id or
// external_ids map holds the given (source, externalID) pair, preferring the
// group's main version (version=1) and excluding excludeID (the track being
// identified). Returns nil when no existing track carries the id.
func (r *TrackRepo) FindByExternalID(ctx context.Context, libraryID, source, externalID, excludeID string) (*domain.Track, error) {
	extJSON, err := externalIDsJSON(map[string]string{source: externalID})
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, title,
		 cover_image_id,
		 duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, audio_codec, external_id, metadata_source, external_ids, acoust_id, hash,
		 lyrics_mask, lyrics_offset, heat, play_count, last_played_at, metadata, version, version_label, created_at, updated_at
		 FROM tracks
		 WHERE library_id = $1 AND id != $4 AND (
		   (metadata_source = $2 AND external_id = $3) OR
		   external_ids @> $5::jsonb
		 )
		 ORDER BY (version = 1) DESC, version ASC, created_at ASC
		 LIMIT 1`,
		libraryID, source, externalID, excludeID, extJSON)
	t, err := scanTrack(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return t, nil
}

// externalIDsJSON renders a source→id map as a JSON object literal for the
// jsonb containment operator, escaping values through the shared marshaler.
// A marshal failure is returned (never a "{}" fallback — a silent "{}" would
// make `external_ids @> '{}'` match nearly every row).
func externalIDsJSON(m map[string]string) (string, error) {
	b, err := marshalExternalIDs(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// externalIDsArg renders a track source→id map for the jsonb column using
// the shared marshaler. A marshal failure is returned rather than silently
// writing the empty object (which would drop the merge data).
func externalIDsArg(m map[string]string) ([]byte, error) {
	b, err := marshalExternalIDs(m)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// VersionGroupMember projects a track that belongs to a version group for
// the group-sync pass: the group key as recorded in track_version_groups plus
// the track's current identity. Loaded in a single query so the sync does not
// issue a version lookup and a full FindByID per member.
type VersionGroupMember struct {
	TrackID     string
	Version     int
	GroupSource string
	GroupExtID  string
	Source      string
	ExternalID  string
	ExternalIDs map[string]string
}

// LoadVersionGroupMembers loads every track referenced by track_version_groups
// together with the group key and the track's current identity.
func (r *TrackRepo) LoadVersionGroupMembers(ctx context.Context, libraryID string) ([]VersionGroupMember, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT g.track_id, t.version, g.metadata_source, g.external_id, t.metadata_source, t.external_id, t.external_ids
		 FROM track_version_groups g
		 JOIN tracks t ON t.id = g.track_id
		 WHERE g.library_id = $1`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []VersionGroupMember
	for rows.Next() {
		var m VersionGroupMember
		var extIDs []byte
		if err := rows.Scan(&m.TrackID, &m.Version, &m.GroupSource, &m.GroupExtID, &m.Source, &m.ExternalID, &extIDs); err != nil {
			return nil, err
		}
		if err := parseExternalIDs(extIDs, &m.ExternalIDs); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// LoadMergeCandidates loads every track of the library together with its
// ordered artists and albums, for the duplicate-merge pass. The artists and
// albums aggregates are computed in two grouped CTE passes (one index scan
// per association table) instead of a correlated subquery per track.
//
// Artists are \x1f-separated; albums use the same unit separator, which
// cannot appear in names or titles, so mergeKey can split on it unambiguously
// (a comma separator would break names like "Earth, Wind & Fire").
func (r *TrackRepo) LoadMergeCandidates(ctx context.Context, libraryID string) ([]MergeTrack, error) {
	rows, err := r.db.QueryContext(ctx,
		`WITH arts AS (
		    SELECT ta.track_id, string_agg(a.name, E'\x1f' ORDER BY ta.sort_order, a.name) AS artists
		    FROM track_artists ta JOIN artists a ON a.id = ta.artist_id
		    GROUP BY ta.track_id
		 ), albs AS (
		    SELECT tal.track_id, string_agg(al.title, E'\x1f' ORDER BY tal.disc_number, tal.track_number) AS albums
		    FROM track_albums tal JOIN albums al ON al.id = tal.album_id
		    GROUP BY tal.track_id
		 )
		 SELECT t.id, t.title, t.metadata_source, t.external_id, t.external_ids, t.version,
		        COALESCE(arts.artists, '') AS artists,
		        COALESCE(albs.albums, '') AS albums
		 FROM tracks t
		 LEFT JOIN arts ON arts.track_id = t.id
		 LEFT JOIN albs ON albs.track_id = t.id
		 WHERE t.library_id = $1`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []MergeTrack
	for rows.Next() {
		var mt MergeTrack
		var extIDs []byte
		if err := rows.Scan(&mt.ID, &mt.Title, &mt.MetadataSource, &mt.ExternalID, &extIDs, &mt.Version, &mt.Artists, &mt.Albums); err != nil {
			return nil, err
		}
		if err := parseExternalIDs(extIDs, &mt.ExternalIDs); err != nil {
			return nil, err
		}
		out = append(out, mt)
	}
	return out, rows.Err()
}

// TrackIDsInVersionGroups returns the set of track ids that already belong
// to a version group (produced by a previous resolveVersions pass).
func (r *TrackRepo) TrackIDsInVersionGroups(ctx context.Context, libraryID string) (map[string]bool, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT track_id FROM track_version_groups WHERE library_id = $1`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	set := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		set[id] = true
	}
	return set, rows.Err()
}

// UpdateMergeFields writes the merged source, primary external id and the
// external_ids map of a track.
func (r *TrackRepo) UpdateMergeFields(ctx context.Context, id, metadataSource, externalID string, externalIDs map[string]string) error {
	ext, err := externalIDsArg(externalIDs)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE tracks SET metadata_source = $1, external_id = $2, external_ids = $3, updated_at = NOW() WHERE id = $4`,
		sourceOrDefault(metadataSource), externalID, ext, id)
	return err
}
