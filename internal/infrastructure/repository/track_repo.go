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
	err := scanner.Scan(&t.ID, &t.LibraryID, &t.Title, &t.AlbumID, &t.ArtistID,
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
		`INSERT INTO tracks (id, library_id, title, album_id, artist_id,
		 cover_image_id,
		 track_number, disc_number, duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, mbid, acoust_id, hash,
		 has_lyrics, lyrics, rating, play_count, metadata, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range tracks {
		_, err = stmt.ExecContext(ctx, t.ID, t.LibraryID, t.Title, t.AlbumID, t.ArtistID,
			t.CoverImageID,
			t.TrackNumber, t.DiscNumber, t.Duration, t.BitRate, t.SampleRate, t.Channels,
			t.FilePath, t.FileSize, t.FileFormat, t.MBID, t.AcoustID, t.Hash,
			t.HasLyrics, t.Lyrics, t.Rating, t.PlayCount, t.Metadata, t.CreatedAt, t.UpdatedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *TrackRepo) FindByID(ctx context.Context, id string) (*domain.Track, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT t.id, t.library_id, t.title, t.album_id, t.artist_id,
		 t.cover_image_id,
		 t.track_number, t.disc_number, t.duration, t.bit_rate, t.sample_rate, t.channels,
		 t.file_path, t.file_size, t.file_format, t.mbid, t.acoust_id, t.hash,
		 t.has_lyrics, t.lyrics, t.rating, t.play_count, t.last_played_at, t.metadata, t.created_at, t.updated_at,
		 COALESCE(a.name,'') as artist_name, COALESCE(al.title,'') as album_title
		 FROM tracks t
		 LEFT JOIN artists a ON a.id = t.artist_id
		 LEFT JOIN albums al ON al.id = t.album_id
		 WHERE t.id = $1`, id)
	return scanTrackWithJoins(row)
}

func (r *TrackRepo) FindByIDs(ctx context.Context, ids []string) ([]*domain.Track, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.id, t.library_id, t.title, t.album_id, t.artist_id,
		 t.cover_image_id,
		 t.track_number, t.disc_number, t.duration, t.bit_rate, t.sample_rate, t.channels,
		 t.file_path, t.file_size, t.file_format, t.mbid, t.acoust_id, t.hash,
		 t.has_lyrics, t.lyrics, t.rating, t.play_count, t.last_played_at, t.metadata, t.created_at, t.updated_at,
		 COALESCE(a.name,'') as artist_name, COALESCE(al.title,'') as album_title
		 FROM tracks t
		 LEFT JOIN artists a ON a.id = t.artist_id
		 LEFT JOIN albums al ON al.id = t.album_id
		 WHERE t.id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*domain.Track
	for rows.Next() {
		t, err := scanTrackWithJoins(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

func scanTrackWithJoins(row interface{ Scan(...interface{}) error }) (*domain.Track, error) {
	t := &domain.Track{}
	var artistName, albumTitle string
	var coverID sql.NullString
	err := row.Scan(&t.ID, &t.LibraryID, &t.Title, &t.AlbumID, &t.ArtistID,
		&coverID,
		&t.TrackNumber, &t.DiscNumber, &t.Duration, &t.BitRate, &t.SampleRate, &t.Channels,
		&t.FilePath, &t.FileSize, &t.FileFormat, &t.MBID, &t.AcoustID, &t.Hash,
		&t.HasLyrics, &t.Lyrics, &t.Rating, &t.PlayCount, &t.LastPlayedAt, &t.Metadata, &t.CreatedAt, &t.UpdatedAt,
		&artistName, &albumTitle)
	if err != nil {
		return nil, err
	}
	if coverID.Valid {
		t.CoverImageID = &coverID.String
	}
	t.Artist = &domain.Artist{Name: artistName}
	t.Album = &domain.Album{Title: albumTitle}
	return t, nil
}

func (r *TrackRepo) FindByLibraryID(ctx context.Context, libraryID string) ([]domain.Track, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, title, album_id, artist_id,
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
		`SELECT id, library_id, title, album_id, artist_id,
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
		`SELECT id, library_id, title, album_id, artist_id,
		 cover_image_id,
		 track_number, disc_number, duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, mbid, acoust_id, hash,
		 has_lyrics, lyrics, rating, play_count, last_played_at, metadata, created_at, updated_at
		 FROM tracks WHERE artist_id = $1`, artistID)
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

func (r *TrackRepo) FindByHash(ctx context.Context, hash string) (*domain.Track, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, title, album_id, artist_id,
		 cover_image_id,
		 track_number, disc_number, duration, bit_rate, sample_rate, channels,
		 file_path, file_size, file_format, mbid, acoust_id, hash,
		 has_lyrics, lyrics, rating, play_count, last_played_at, metadata, created_at, updated_at
		 FROM tracks WHERE hash = $1`, hash)
	return scanTrack(row)
}

func (r *TrackRepo) Update(ctx context.Context, track *domain.Track) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE tracks SET title=$1, album_id=$2, artist_id=$3, cover_image_id=$4,
		 track_number=$5, disc_number=$6, duration=$7, bit_rate=$8, sample_rate=$9, channels=$10,
		 file_path=$11, file_size=$12, file_format=$13, mbid=$14, acoust_id=$15,
		 hash=$16, has_lyrics=$17, lyrics=$18, rating=$19, play_count=$20,
		 last_played_at=$21, metadata=$22, updated_at=NOW()
		 WHERE id=$23`,
		track.Title, track.AlbumID, track.ArtistID, track.CoverImageID,
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


