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
	err := scanner.Scan(&a.ID, &a.LibraryID, &a.Name, &a.SortName,
		&a.MBID, &a.Biography, &coverID, &a.AlbumCount,
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
		`INSERT INTO artists (id, library_id, name, sort_name, mbid, biography, cover_image_id, album_count, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, updated_at=NOW()`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, a := range artists {
		_, err = stmt.ExecContext(ctx, a.ID, a.LibraryID, a.Name, a.SortName,
			a.MBID, a.Biography, a.CoverImageID, a.AlbumCount, a.CreatedAt, a.UpdatedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *ArtistRepo) FindByID(ctx context.Context, id string) (*domain.Artist, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, name, sort_name, mbid, biography, cover_image_id, album_count, created_at, updated_at
		 FROM artists WHERE id = $1`, id)
	return scanArtist(row)
}

func (r *ArtistRepo) FindByLibraryID(ctx context.Context, libraryID string) ([]domain.Artist, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, name, sort_name, mbid, biography, cover_image_id, album_count, created_at, updated_at
		 FROM artists WHERE library_id = $1 ORDER BY name ASC`, libraryID)
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

func (r *ArtistRepo) FindByNameAndLibrary(ctx context.Context, name, libraryID string) (*domain.Artist, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, name, sort_name, mbid, biography, cover_image_id, album_count, created_at, updated_at
		 FROM artists WHERE name = $1 AND library_id = $2`, name, libraryID)
	return scanArtist(row)
}

func (r *ArtistRepo) Update(ctx context.Context, artist *domain.Artist) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE artists SET name=$1, sort_name=$2, mbid=$3, biography=$4,
		 cover_image_id=$5, album_count=$6, updated_at=NOW()
		 WHERE id=$7`,
		artist.Name, artist.SortName, artist.MBID, artist.Biography,
		artist.CoverImageID, artist.AlbumCount, artist.ID)
	return err
}
