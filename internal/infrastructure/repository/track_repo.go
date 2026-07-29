package repository

import (
	"context"
	"database/sql"

	"github.com/lib/pq"
	"github.com/sonicore/server/internal/core/domain"
)

type TrackRepo struct {
	db *sql.DB
}

func NewTrackRepo(db *sql.DB) *TrackRepo {
	return &TrackRepo{db: db}
}

func scanTrack(scanner interface{ Scan(dest ...interface{}) error }) (*domain.Track, error) {
	var t domain.Track
	var metadata sql.NullString
	var coverID sql.NullString
	err := scanner.Scan(&t.ID, &t.LibraryID, &t.Title, &t.AlbumID,
		&coverID,
		&t.TrackNumber, &t.DiscNumber, &t.Duration, &t.BitRate, &t.SampleRate,
		&t.Channels, &t.FilePath, &t.FileSize, &t.FileFormat, &t.MBID, &t.AcoustID,
		&t.Hash, &t.HasLyrics, &t.Lyrics, &t.Rating, &t.PlayCount, &t.LastPlayedAt,
		&metadata, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if coverID.Valid {
		t.CoverImageID = &coverID.String
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
		`INSERT INTO tracks (id, library_id, title, album_id,
		 cover_image_id,
		 track_number, disc_number, duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, mbid, acoust_id, hash,
		 has_lyrics, lyrics, rating, play_count, metadata, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)`)
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

	for _, t := range tracks {
		_, err = stmt.ExecContext(ctx, t.ID, t.LibraryID, t.Title, t.AlbumID,
			t.CoverImageID,
			t.TrackNumber, t.DiscNumber, t.Duration, t.BitRate, t.SampleRate, t.Channels,
			t.FilePath, t.FileSize, t.FileFormat, t.MBID, t.AcoustID, t.Hash,
			t.HasLyrics, t.Lyrics, t.Rating, t.PlayCount, t.Metadata, t.CreatedAt, t.UpdatedAt)
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
	}

	return tx.Commit()
}

func (r *TrackRepo) FindByID(ctx context.Context, id string) (*domain.Track, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT t.id, t.library_id, t.title, t.album_id,
		 t.cover_image_id,
		 t.track_number, t.disc_number, t.duration, t.bit_rate, t.sample_rate, t.channels,
		 t.file_path, t.file_size, t.file_format, t.mbid, t.acoust_id, t.hash,
		 t.has_lyrics, t.lyrics, t.rating, t.play_count, t.last_played_at, t.metadata, t.created_at, t.updated_at,
		 COALESCE(al.title,'') as album_title
		 FROM tracks t
		 LEFT JOIN albums al ON al.id = t.album_id
		 WHERE t.id = $1`, id)
	t, err := scanTrackWithAlbum(row)
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
		`SELECT t.id, t.library_id, t.title, t.album_id,
		 t.cover_image_id,
		 t.track_number, t.disc_number, t.duration, t.bit_rate, t.sample_rate, t.channels,
		 t.file_path, t.file_size, t.file_format, t.mbid, t.acoust_id, t.hash,
		 t.has_lyrics, t.lyrics, t.rating, t.play_count, t.last_played_at, t.metadata, t.created_at, t.updated_at,
		 COALESCE(al.title,'') as album_title
		 FROM tracks t
		 LEFT JOIN albums al ON al.id = t.album_id
		 WHERE t.id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*domain.Track
	for rows.Next() {
		t, err := scanTrackWithAlbum(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Bulk-load track_artists for all tracks
	trackArtists, err := r.LoadTrackArtistsBulk(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, t := range list {
		t.Artists = trackArtists[t.ID]
		if len(t.Artists) > 0 {
			t.Artist = t.Artists[0].Artist
		}
	}
	return list, nil
}

type trackArtistRow struct {
	TrackID   string
	ArtistID  string
	Role      string
	SortOrder int
	Name      string
	MBID      string
}

func (r *TrackRepo) LoadTrackArtists(ctx context.Context, trackID string) ([]*domain.TrackArtist, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT ta.track_id, ta.artist_id, ta.role, ta.sort_order, a.name, a.mbid
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
		if err := rows.Scan(&row.TrackID, &row.ArtistID, &row.Role, &row.SortOrder, &row.Name, &row.MBID); err != nil {
			return nil, err
		}
		artists = append(artists, &domain.TrackArtist{
			TrackID:   row.TrackID,
			ArtistID:  row.ArtistID,
			Role:      row.Role,
			SortOrder: row.SortOrder,
			Artist:    &domain.Artist{ID: row.ArtistID, Name: row.Name, MBID: row.MBID},
		})
	}
	return artists, rows.Err()
}

func (r *TrackRepo) LoadTrackArtistsBulk(ctx context.Context, trackIDs []string) (map[string][]*domain.TrackArtist, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT ta.track_id, ta.artist_id, ta.role, ta.sort_order, a.name, a.mbid
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
		if err := rows.Scan(&row.TrackID, &row.ArtistID, &row.Role, &row.SortOrder, &row.Name, &row.MBID); err != nil {
			return nil, err
		}
		result[row.TrackID] = append(result[row.TrackID], &domain.TrackArtist{
			TrackID:   row.TrackID,
			ArtistID:  row.ArtistID,
			Role:      row.Role,
			SortOrder: row.SortOrder,
			Artist:    &domain.Artist{ID: row.ArtistID, Name: row.Name, MBID: row.MBID},
		})
	}
	return result, rows.Err()
}

func scanTrackWithAlbum(row interface{ Scan(...interface{}) error }) (*domain.Track, error) {
	t := &domain.Track{}
	var albumTitle string
	var coverID sql.NullString
	err := row.Scan(&t.ID, &t.LibraryID, &t.Title, &t.AlbumID,
		&coverID,
		&t.TrackNumber, &t.DiscNumber, &t.Duration, &t.BitRate, &t.SampleRate, &t.Channels,
		&t.FilePath, &t.FileSize, &t.FileFormat, &t.MBID, &t.AcoustID, &t.Hash,
		&t.HasLyrics, &t.Lyrics, &t.Rating, &t.PlayCount, &t.LastPlayedAt, &t.Metadata, &t.CreatedAt, &t.UpdatedAt,
		&albumTitle)
	if err != nil {
		return nil, err
	}
	if coverID.Valid {
		t.CoverImageID = &coverID.String
	}
	t.Album = &domain.Album{Title: albumTitle}
	return t, nil
}

func (r *TrackRepo) FindByLibraryID(ctx context.Context, libraryID string) ([]domain.Track, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, title, album_id,
		 cover_image_id,
		 track_number, disc_number, duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, mbid, acoust_id, hash,
		 has_lyrics, lyrics, rating, play_count, last_played_at, metadata, created_at, updated_at
		 FROM tracks WHERE library_id = $1 ORDER BY disc_number, track_number`, libraryID)
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
		`SELECT id, library_id, title, album_id,
		 cover_image_id,
		 track_number, disc_number, duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, mbid, acoust_id, hash,
		 has_lyrics, lyrics, rating, play_count, last_played_at, metadata, created_at, updated_at
		 FROM tracks WHERE album_id = $1 ORDER BY disc_number, track_number`, albumID)
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
		`SELECT t.id, t.library_id, t.title, t.album_id,
		 t.cover_image_id,
		 t.track_number, t.disc_number, t.duration, t.bit_rate, t.sample_rate, t.channels,
		 t.file_path, t.file_size, t.file_format, t.mbid, t.acoust_id, t.hash,
		 t.has_lyrics, t.lyrics, t.rating, t.play_count, t.last_played_at, t.metadata, t.created_at, t.updated_at
		 FROM tracks t
		 INNER JOIN track_artists ta ON ta.track_id = t.id
		 WHERE ta.artist_id = $1
		 ORDER BY t.disc_number, t.track_number`, artistID)
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

	// Load track_artists for all returned tracks
	if len(trackIDs) > 0 {
		tas, _ := r.LoadTrackArtistsBulk(ctx, trackIDs)
		for i := range tracks {
			tracks[i].Artists = tas[tracks[i].ID]
		}
	}
	return tracks, rows.Err()
}

func (r *TrackRepo) FindByHash(ctx context.Context, hash string) (*domain.Track, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, title, album_id,
		 cover_image_id,
		 track_number, disc_number, duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, mbid, acoust_id, hash,
		 has_lyrics, lyrics, rating, play_count, last_played_at, metadata, created_at, updated_at
		 FROM tracks WHERE hash = $1`, hash)
	return scanTrack(row)
}

func (r *TrackRepo) Update(ctx context.Context, track *domain.Track) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tracks SET title=$1, album_id=$2, cover_image_id=$3,
		 track_number=$4, disc_number=$5, duration=$6, bit_rate=$7, sample_rate=$8, channels=$9,
		 file_path=$10, file_size=$11, file_format=$12, mbid=$13, acoust_id=$14,
		 hash=$15, has_lyrics=$16, lyrics=$17, rating=$18, play_count=$19,
		 last_played_at=$20, metadata=$21, updated_at=NOW()
		 WHERE id=$22`,
		track.Title, track.AlbumID, track.CoverImageID,
		track.TrackNumber, track.DiscNumber, track.Duration, track.BitRate, track.SampleRate, track.Channels,
		track.FilePath, track.FileSize, track.FileFormat, track.MBID, track.AcoustID,
		track.Hash, track.HasLyrics, track.Lyrics, track.Rating, track.PlayCount,
		track.LastPlayedAt, track.Metadata, track.ID)
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


