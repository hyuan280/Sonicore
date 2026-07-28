package repository

import (
	"context"
	"database/sql"
)

type UserMetadata struct {
	UserID      string
	FileHash    string
	TrackMBID   string
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	TrackNumber int
	DiscNumber  int
	Year        int
	Genre       string
}

type UserMetadataRepo struct {
	db *sql.DB
}

func NewUserMetadataRepo(db *sql.DB) *UserMetadataRepo {
	return &UserMetadataRepo{db: db}
}

func (r *UserMetadataRepo) FindByUserAndHash(ctx context.Context, userID, hash string) (*UserMetadata, error) {
	var m UserMetadata
	err := r.db.QueryRowContext(ctx,
		`SELECT user_id, file_hash, track_mbid, title, artist, album, album_artist,
		 track_number, disc_number, year, genre
		 FROM user_metadata WHERE user_id = $1 AND file_hash = $2`, userID, hash).
		Scan(&m.UserID, &m.FileHash, &m.TrackMBID, &m.Title, &m.Artist, &m.Album, &m.AlbumArtist,
			&m.TrackNumber, &m.DiscNumber, &m.Year, &m.Genre)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *UserMetadataRepo) Upsert(ctx context.Context, m *UserMetadata) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO user_metadata (user_id, file_hash, track_mbid, title, artist, album, album_artist, track_number, disc_number, year, genre, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW())
		 ON CONFLICT (user_id, file_hash) DO UPDATE SET
		 track_mbid=$3, title=$4, artist=$5, album=$6, album_artist=$7,
		 track_number=$8, disc_number=$9, year=$10, genre=$11, updated_at=NOW()`,
		m.UserID, m.FileHash, m.TrackMBID, m.Title, m.Artist, m.Album, m.AlbumArtist,
		m.TrackNumber, m.DiscNumber, m.Year, m.Genre)
	return err
}
