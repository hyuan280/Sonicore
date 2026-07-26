package repository

import (
	"context"
	"database/sql"

	"github.com/sonicore/server/internal/core/domain"
)

type AlbumRepo struct {
	db *sql.DB
}

func NewAlbumRepo(db *sql.DB) *AlbumRepo {
	return &AlbumRepo{db: db}
}

func scanAlbum(scanner interface{ Scan(dest ...interface{}) error }) (*domain.Album, error) {
	var a domain.Album
	var coverID sql.NullString
	err := scanner.Scan(&a.ID, &a.LibraryID, &a.Title, &a.ArtistID,
		&a.MBID, &a.Year, &a.Genre, &coverID,
		&a.SongCount, &a.Duration, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if coverID.Valid {
		a.CoverImageID = &coverID.String
	}
	return &a, nil
}

func (r *AlbumRepo) BatchCreate(ctx context.Context, albums []domain.Album) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO albums (id, library_id, title, artist_id, mbid, year, genre, cover_image_id, song_count, duration, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		 ON CONFLICT (id) DO UPDATE SET title=EXCLUDED.title, updated_at=NOW()`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, a := range albums {
		_, err = stmt.ExecContext(ctx, a.ID, a.LibraryID, a.Title, a.ArtistID,
			a.MBID, a.Year, a.Genre, a.CoverImageID, a.SongCount, a.Duration,
			a.CreatedAt, a.UpdatedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *AlbumRepo) FindByID(ctx context.Context, id string) (*domain.Album, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, title, artist_id, mbid, year, genre, cover_image_id, song_count, duration, created_at, updated_at
		 FROM albums WHERE id = $1`, id)
	return scanAlbum(row)
}

func (r *AlbumRepo) FindByLibraryID(ctx context.Context, libraryID string) ([]domain.Album, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, title, artist_id, mbid, year, genre, cover_image_id, song_count, duration, created_at, updated_at
		 FROM albums WHERE library_id = $1 ORDER BY year DESC, title ASC`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var albums []domain.Album
	for rows.Next() {
		a, err := scanAlbum(rows)
		if err != nil {
			return nil, err
		}
		albums = append(albums, *a)
	}
	return albums, rows.Err()
}

func (r *AlbumRepo) FindByArtistID(ctx context.Context, artistID string) ([]domain.Album, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, title, artist_id, mbid, year, genre, cover_image_id, song_count, duration, created_at, updated_at
		 FROM albums WHERE artist_id = $1 ORDER BY year DESC`, artistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var albums []domain.Album
	for rows.Next() {
		a, err := scanAlbum(rows)
		if err != nil {
			return nil, err
		}
		albums = append(albums, *a)
	}
	return albums, rows.Err()
}

func (r *AlbumRepo) FindByNameAndArtist(ctx context.Context, name, artistID, libraryID string) (*domain.Album, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, title, artist_id, mbid, year, genre, cover_image_id, song_count, duration, created_at, updated_at
		 FROM albums WHERE title = $1 AND artist_id = $2 AND library_id = $3`,
		name, artistID, libraryID)
	return scanAlbum(row)
}

func (r *AlbumRepo) Update(ctx context.Context, album *domain.Album) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE albums SET title=$1, artist_id=$2, mbid=$3, year=$4, genre=$5,
		 cover_image_id=$6, song_count=$7, duration=$8, updated_at=NOW()
		 WHERE id=$9`,
		album.Title, album.ArtistID, album.MBID, album.Year, album.Genre,
		album.CoverImageID, album.SongCount, album.Duration, album.ID)
	return err
}
