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
	err := scanner.Scan(&t.ID, &t.LibraryID, &t.Title,
		&coverID,
		&t.Duration, &t.BitRate, &t.SampleRate,
		&t.Channels, &t.FilePath, &t.FileSize, &t.FileFormat, &t.AudioCodec, &t.MBID, &t.AcoustID,
		&t.Hash, &t.LyricsMask, &t.Lyrics, &t.Rating, &t.PlayCount, &t.LastPlayedAt,
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
		`INSERT INTO tracks (id, library_id, title,
		 cover_image_id,
		 duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, audio_codec, mbid, acoust_id, hash,
		 lyrics_mask, lyrics, rating, play_count, metadata, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`)
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
		_, err = stmt.ExecContext(ctx, t.ID, t.LibraryID, t.Title,
			t.CoverImageID,
			t.Duration, t.BitRate, t.SampleRate, t.Channels,
			t.FilePath, t.FileSize, t.FileFormat, t.AudioCodec, t.MBID, t.AcoustID, t.Hash,
			t.LyricsMask, t.Lyrics, t.Rating, t.PlayCount, t.Metadata, t.CreatedAt, t.UpdatedAt)
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
		 file_path, file_size, file_format, audio_codec, mbid, acoust_id, hash,
		 lyrics_mask, lyrics, rating, play_count, last_played_at, metadata, created_at, updated_at
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
		 file_path, file_size, file_format, audio_codec, mbid, acoust_id, hash,
		 lyrics_mask, lyrics, rating, play_count, last_played_at, metadata, created_at, updated_at
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

func (r *TrackRepo) FindByLibraryID(ctx context.Context, libraryID string) ([]domain.Track, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, title,
		 cover_image_id,
		 duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, audio_codec, mbid, acoust_id, hash,
		 lyrics_mask, lyrics, rating, play_count, last_played_at, metadata, created_at, updated_at
		 FROM tracks WHERE library_id = $1`, libraryID)
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
		 t.file_path, t.file_size, t.file_format, t.audio_codec, t.mbid, t.acoust_id, t.hash,
		 t.lyrics_mask, t.lyrics, t.rating, t.play_count, t.last_played_at, t.metadata, t.created_at, t.updated_at
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
		 t.file_path, t.file_size, t.file_format, t.audio_codec, t.mbid, t.acoust_id, t.hash,
		 t.lyrics_mask, t.lyrics, t.rating, t.play_count, t.last_played_at, t.metadata, t.created_at, t.updated_at
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
		 file_path, file_size, file_format, audio_codec, mbid, acoust_id, hash,
		 lyrics_mask, lyrics, rating, play_count, last_played_at, metadata, created_at, updated_at
		 FROM tracks WHERE hash = $1`, hash)
	return scanTrack(row)
}

func (r *TrackRepo) Update(ctx context.Context, track *domain.Track) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`UPDATE tracks SET title=$1, cover_image_id=$2,
		 duration=$3, bit_rate=$4, sample_rate=$5, channels=$6,
		 file_path=$7, file_size=$8, file_format=$9, audio_codec=$10, mbid=$11, acoust_id=$12,
		 hash=$13, lyrics_mask=$14, lyrics=$15, rating=$16, play_count=$17,
		 last_played_at=$18, metadata=$19, updated_at=NOW()
		 WHERE id=$20`,
		track.Title, track.CoverImageID,
		track.Duration, track.BitRate, track.SampleRate, track.Channels,
		track.FilePath, track.FileSize, track.FileFormat, track.AudioCodec, track.MBID, track.AcoustID,
		track.Hash, track.LyricsMask, track.Lyrics, track.Rating, track.PlayCount,
		track.LastPlayedAt, track.Metadata, track.ID)
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
