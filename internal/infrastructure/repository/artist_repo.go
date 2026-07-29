package repository

import (
	"context"
	"database/sql"

	"github.com/sonicore/server/internal/core/domain"
)

type ArtistRepo struct {
	db *sql.DB
}

func NewArtistRepo(db *sql.DB) *ArtistRepo {
	return &ArtistRepo{db: db}
}

func scanArtist(scanner interface{ Scan(dest ...interface{}) error }) (*domain.Artist, error) {
	var a domain.Artist
	var coverID sql.NullString
	err := scanner.Scan(&a.ID, &a.Name, &a.SortName,
		&a.MBID, &a.Country, &a.Biography, &coverID, &a.TrackCount,
		&a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if coverID.Valid {
		a.CoverImageID = &coverID.String
	}
	return &a, nil
}

func (r *ArtistRepo) BatchCreate(ctx context.Context, artists []domain.Artist) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO artists (id, name, sort_name, mbid, country, biography, cover_image_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		 ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, updated_at=NOW()`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, a := range artists {
		_, err = stmt.ExecContext(ctx, a.ID, a.Name, a.SortName,
			a.MBID, a.Country, a.Biography, a.CoverImageID, a.CreatedAt, a.UpdatedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *ArtistRepo) FindByID(ctx context.Context, id string) (*domain.Artist, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, sort_name, mbid, country, biography, cover_image_id, 0 AS track_count, created_at, updated_at
		 FROM artists WHERE id = $1`, id)
	return scanArtist(row)
}

func (r *ArtistRepo) FindByLibraryID(ctx context.Context, libraryID string) ([]domain.Artist, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT a.id, a.name, a.sort_name, a.mbid, a.country, a.biography, a.cover_image_id, 0 AS track_count, a.created_at, a.updated_at
		 FROM artists a
		 INNER JOIN tracks t ON t.artist_id = a.id
		 WHERE t.library_id = $1
		 ORDER BY a.name ASC`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artists []domain.Artist
	for rows.Next() {
		a, err := scanArtist(rows)
		if err != nil {
			return nil, err
		}
		artists = append(artists, *a)
	}
	return artists, rows.Err()
}

func (r *ArtistRepo) FindAccessible(ctx context.Context, userID string) ([]domain.Artist, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT a.id, a.name, a.sort_name, a.mbid, a.country, a.biography, a.cover_image_id,
		 (SELECT COUNT(*) FROM tracks
		  INNER JOIN library_members lm2 ON lm2.library_id = tracks.library_id
		  WHERE tracks.artist_id = a.id AND lm2.user_id = $1) AS track_count,
		 a.created_at, a.updated_at
		 FROM artists a
		 INNER JOIN tracks t ON t.artist_id = a.id
		 INNER JOIN library_members lm ON lm.library_id = t.library_id
		 WHERE lm.user_id = $1
		 ORDER BY a.name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artists []domain.Artist
	for rows.Next() {
		a, err := scanArtist(rows)
		if err != nil {
			return nil, err
		}
		artists = append(artists, *a)
	}
	return artists, rows.Err()
}

func (r *ArtistRepo) FindByMBID(ctx context.Context, mbid string) (*domain.Artist, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, sort_name, mbid, country, biography, cover_image_id, 0 AS track_count, created_at, updated_at
		 FROM artists WHERE mbid = $1`, mbid)
	return scanArtist(row)
}

func (r *ArtistRepo) FindByNameAndLibrary(ctx context.Context, name, libraryID string) (*domain.Artist, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, sort_name, mbid, country, biography, cover_image_id, 0 AS track_count, created_at, updated_at
		 FROM artists WHERE name = $1`, name)
	return scanArtist(row)
}

func (r *ArtistRepo) FindByName(ctx context.Context, name string) (*domain.Artist, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, sort_name, mbid, country, biography, cover_image_id, 0 AS track_count, created_at, updated_at
		 FROM artists WHERE name = $1`, name)
	return scanArtist(row)
}

func (r *ArtistRepo) Update(ctx context.Context, artist *domain.Artist) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE artists SET name=$1, sort_name=$2, mbid=$3, country=$4, biography=$5,
		 cover_image_id=$6, updated_at=NOW()
		 WHERE id=$7`,
		artist.Name, artist.SortName, artist.MBID, artist.Country, artist.Biography,
		artist.CoverImageID, artist.ID)
	return err
}
