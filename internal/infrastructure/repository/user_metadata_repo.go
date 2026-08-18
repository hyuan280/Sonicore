package repository

import (
	"context"
	"database/sql"
)

// UserMetadata is the per-user saved metadata cache. metadata_source and
// external_id record which source the cache is based on (e.g. a track that
// already had a MusicBrainz id keeps source=musicbrainz + that id), so the
// user-metadata source can re-provide them.
type UserMetadata struct {
	UserID         string
	FileHash       string
	MetadataSource string
	ExternalID     string
	Title          string
	Artist         string
	Album          string
	AlbumArtist    string
	TrackNumber    int
	DiscNumber     int
	Year           int
	Genre          string
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
		`SELECT user_id, file_hash, metadata_source, external_id, title, artist, album, album_artist,
		 track_number, disc_number, year, genre
		 FROM user_metadata WHERE user_id = $1 AND file_hash = $2`, userID, hash).
		Scan(&m.UserID, &m.FileHash, &m.MetadataSource, &m.ExternalID, &m.Title, &m.Artist, &m.Album, &m.AlbumArtist,
			&m.TrackNumber, &m.DiscNumber, &m.Year, &m.Genre)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *UserMetadataRepo) Upsert(ctx context.Context, m *UserMetadata) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO user_metadata (user_id, file_hash, metadata_source, external_id, title, artist, album, album_artist, track_number, disc_number, year, genre, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW())
		 ON CONFLICT (user_id, file_hash) DO UPDATE SET
		 metadata_source=$3, external_id=$4, title=$5, artist=$6, album=$7, album_artist=$8,
		 track_number=$9, disc_number=$10, year=$11, genre=$12, updated_at=NOW()`,
		m.UserID, m.FileHash, m.MetadataSource, m.ExternalID, m.Title, m.Artist, m.Album, m.AlbumArtist,
		m.TrackNumber, m.DiscNumber, m.Year, m.Genre)
	return err
}

func (r *UserMetadataRepo) DeleteByUserAndHash(ctx context.Context, userID, fileHash string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM user_metadata WHERE user_id = $1 AND file_hash = $2`,
		userID, fileHash)
	return err
}
