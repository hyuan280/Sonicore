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
	err := scanner.Scan(&a.ID, &a.Title, &a.ArtistID,
		&a.MBID, &a.Country, &a.Year, &a.Genre, &coverID,
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
		`INSERT INTO albums (id, title, artist_id, mbid, country, year, genre, cover_image_id, song_count, duration, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		 ON CONFLICT (id) DO UPDATE SET title=EXCLUDED.title, updated_at=NOW()`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, a := range albums {
		_, err = stmt.ExecContext(ctx, a.ID, a.Title, a.ArtistID,
			a.MBID, a.Country, a.Year, a.Genre, a.CoverImageID, a.SongCount, a.Duration,
			a.CreatedAt, a.UpdatedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *AlbumRepo) FindByID(ctx context.Context, id string) (*domain.Album, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, title, artist_id, mbid, country, year, genre, cover_image_id, song_count, duration, created_at, updated_at
		 FROM albums WHERE id = $1`, id)
	return scanAlbum(row)
}

func (r *AlbumRepo) FindByLibraryID(ctx context.Context, libraryID string) ([]domain.Album, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT a.id, a.title, a.artist_id, a.mbid, a.country, a.year, a.genre, a.cover_image_id, a.song_count, a.duration, a.created_at, a.updated_at
		 FROM albums a
		 INNER JOIN tracks t ON t.album_id = a.id
		 WHERE t.library_id = $1
		 ORDER BY a.year DESC, a.title ASC`, libraryID)
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
		`SELECT id, title, artist_id, mbid, country, year, genre, cover_image_id, song_count, duration, created_at, updated_at
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
		`SELECT id, title, artist_id, mbid, country, year, genre, cover_image_id, song_count, duration, created_at, updated_at
		 FROM albums WHERE title = $1 AND artist_id = $2`,
		name, artistID)
	return scanAlbum(row)
}

func (r *AlbumRepo) FindByName(ctx context.Context, name string) (*domain.Album, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, title, artist_id, mbid, country, year, genre, cover_image_id, song_count, duration, created_at, updated_at
		 FROM albums WHERE title = $1`, name)
	return scanAlbum(row)
}

func (r *AlbumRepo) FindByTitleAndArtist(ctx context.Context, title, artistID string) (*domain.Album, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, title, artist_id, mbid, country, year, genre, cover_image_id, song_count, duration, created_at, updated_at
		 FROM albums WHERE title = $1 AND artist_id = $2`, title, artistID)
	return scanAlbum(row)
}

func (r *AlbumRepo) FindByMBID(ctx context.Context, mbid string) (*domain.Album, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, title, artist_id, mbid, country, year, genre, cover_image_id, song_count, duration, created_at, updated_at
		 FROM albums WHERE mbid = $1`, mbid)
	return scanAlbum(row)
}

func (r *AlbumRepo) FindAccessible(ctx context.Context, userID string) ([]domain.Album, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT al.id, al.title, al.artist_id, al.mbid, al.country, al.year, al.genre, al.cover_image_id, al.song_count, al.duration, al.created_at, al.updated_at
		 FROM albums al
		 INNER JOIN tracks t ON t.album_id = al.id
		 INNER JOIN library_members lm ON lm.library_id = t.library_id
		 WHERE lm.user_id = $1
		 ORDER BY al.year DESC, al.title ASC`, userID)
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

func (r *AlbumRepo) Update(ctx context.Context, album *domain.Album) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE albums SET title=$1, artist_id=$2, mbid=$3, country=$4, year=$5, genre=$6,
		 cover_image_id=$7, song_count=$8, duration=$9, updated_at=NOW()
		 WHERE id=$10`,
		album.Title, album.ArtistID, album.MBID, album.Country, album.Year, album.Genre,
		album.CoverImageID, album.SongCount, album.Duration, album.ID)
	return err
}
