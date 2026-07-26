package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/sonicore/server/internal/core/domain"
)

type PlaylistRepo struct {
	db *sql.DB
}

func NewPlaylistRepo(db *sql.DB) *PlaylistRepo {
	return &PlaylistRepo{db: db}
}

func (r *PlaylistRepo) Create(ctx context.Context, p *domain.Playlist) error {
	trackIDs, _ := json.Marshal(p.TrackIDs)
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO playlists (id, name, owner_id, is_public, track_ids, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		p.ID, p.Name, p.OwnerID, p.IsPublic, trackIDs, p.CreatedAt, p.UpdatedAt)
	return err
}

func (r *PlaylistRepo) FindByID(ctx context.Context, id string) (*domain.Playlist, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, owner_id, is_public, track_ids, created_at, updated_at
		 FROM playlists WHERE id = $1`, id)
	return scanPlaylist(row)
}

func (r *PlaylistRepo) FindByLibraryID(ctx context.Context, libraryID string) ([]domain.Playlist, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, owner_id, is_public, track_ids, created_at, updated_at
		 FROM playlists ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPlaylists(rows)
}

func (r *PlaylistRepo) FindByUserID(ctx context.Context, userID string) ([]domain.Playlist, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, owner_id, is_public, track_ids, created_at, updated_at
		 FROM playlists WHERE owner_id = $1 ORDER BY name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPlaylists(rows)
}

func (r *PlaylistRepo) Update(ctx context.Context, p *domain.Playlist) error {
	trackIDs, _ := json.Marshal(p.TrackIDs)
	_, err := r.db.ExecContext(ctx,
		`UPDATE playlists SET name=$1, is_public=$2, track_ids=$3, updated_at=NOW()
		 WHERE id=$4`,
		p.Name, p.IsPublic, trackIDs, p.ID)
	return err
}

func (r *PlaylistRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM playlists WHERE id = $1`, id)
	return err
}

func scanPlaylist(scanner interface{ Scan(dest ...interface{}) error }) (*domain.Playlist, error) {
	var p domain.Playlist
	var trackIDs []byte
	err := scanner.Scan(&p.ID, &p.Name, &p.OwnerID,
		&p.IsPublic, &trackIDs, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(trackIDs, &p.TrackIDs)
	return &p, nil
}

func scanPlaylists(rows *sql.Rows) ([]domain.Playlist, error) {
	var playlists []domain.Playlist
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			return nil, err
		}
		playlists = append(playlists, *p)
	}
	return playlists, rows.Err()
}
