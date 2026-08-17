package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/pkg/utils"
)

type AlbumRepo struct {
	db *sql.DB
}

func NewAlbumRepo(db *sql.DB) *AlbumRepo {
	return &AlbumRepo{db: db}
}

// albumCols is the canonical column list for scanAlbum. Keep in sync with the
// scan order in scanAlbum.
const albumCols = `id, title, artist_id, external_id, metadata_source, external_ids, country, year, genre, cover_image_id, song_count, duration, created_at, updated_at`

// albumColsPrefixed is albumCols qualified with the al. alias for JOIN queries.
const albumColsPrefixed = `al.id, al.title, al.artist_id, al.external_id, al.metadata_source, al.external_ids, al.country, al.year, al.genre, al.cover_image_id, al.song_count, al.duration, al.created_at, al.updated_at`

func scanAlbum(scanner interface{ Scan(dest ...interface{}) error }) (*domain.Album, error) {
	var a domain.Album
	var coverID sql.NullString
	var ext []byte
	err := scanner.Scan(&a.ID, &a.Title, &a.ArtistID,
		&a.ExternalID, &a.MetadataSource, &ext, &a.Country, &a.Year, &a.Genre, &coverID,
		&a.SongCount, &a.Duration, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if len(ext) > 0 {
		var m map[string]string
		if err := json.Unmarshal(ext, &m); err != nil {
			return nil, fmt.Errorf("decode external_ids for album %s: %w", a.ID, err)
		}
		a.ExternalIDs = m
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
		`INSERT INTO albums (id, title, artist_id, external_id, metadata_source, external_ids, title_normalized, country, year, genre, cover_image_id, song_count, duration, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		 ON CONFLICT (id) DO UPDATE SET title=EXCLUDED.title, title_normalized=EXCLUDED.title_normalized,
		 metadata_source=EXCLUDED.metadata_source, external_ids=EXCLUDED.external_ids, updated_at=NOW()`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, a := range albums {
		ext, err := marshalExternalIDs(a.ExternalIDs)
		if err != nil {
			return err
		}
		_, err = stmt.ExecContext(ctx, a.ID, a.Title, a.ArtistID,
			a.ExternalID, sourceOrDefault(a.MetadataSource), ext, utils.NormalizeName(a.Title),
			a.Country, a.Year, a.Genre, a.CoverImageID, a.SongCount, a.Duration,
			a.CreatedAt, a.UpdatedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *AlbumRepo) FindByID(ctx context.Context, id string) (*domain.Album, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+albumCols+`
		 FROM albums WHERE id = $1`, id)
	return scanAlbum(row)
}

func (r *AlbumRepo) FindByLibraryID(ctx context.Context, libraryID string) ([]domain.Album, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT `+albumColsPrefixed+`
		 FROM albums al
		 INNER JOIN track_albums ta ON ta.album_id = al.id
		 INNER JOIN tracks t ON t.id = ta.track_id
		 WHERE t.library_id = $1
		 ORDER BY al.year DESC, al.title ASC`, libraryID)
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
		`SELECT `+albumCols+` FROM albums WHERE artist_id = $1 ORDER BY year DESC`, artistID)
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
		`SELECT `+albumCols+` FROM albums WHERE title = $1 AND artist_id = $2`,
		name, artistID)
	return scanAlbum(row)
}

func (r *AlbumRepo) FindByName(ctx context.Context, name string) (*domain.Album, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+albumCols+` FROM albums WHERE title = $1`, name)
	return scanAlbum(row)
}

func (r *AlbumRepo) FindByTitleAndArtist(ctx context.Context, title, artistID string) (*domain.Album, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+albumCols+` FROM albums WHERE title = $1 AND artist_id = $2`, title, artistID)
	return scanAlbum(row)
}

// FindByTitleNormalizedAndArtist looks up an album by canonical title within
// the owning artist. ORDER BY makes the pick deterministic when duplicates
// exist (no unique constraint on (artist_id, title_normalized)).
func (r *AlbumRepo) FindByTitleNormalizedAndArtist(ctx context.Context, title, artistID string) (*domain.Album, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+albumCols+` FROM albums WHERE title_normalized = $1 AND artist_id = $2
		 ORDER BY created_at, id LIMIT 1`,
		utils.NormalizeName(title), artistID)
	return scanAlbum(row)
}

// FindByMBID looks up an album by its primary external ID, restricted to the
// given source (defaults to musicbrainz for backward compatibility).
func (r *AlbumRepo) FindByMBID(ctx context.Context, externalID string) (*domain.Album, error) {
	return r.FindBySourceAndID(ctx, "musicbrainz", externalID)
}

// FindBySourceAndID looks up an album by its primary external ID within a
// source (metadata_source + external_id).
func (r *AlbumRepo) FindBySourceAndID(ctx context.Context, source, id string) (*domain.Album, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+albumCols+` FROM albums WHERE metadata_source = $1 AND external_id = $2`,
		sourceOrDefault(source), id)
	return scanAlbum(row)
}

// FindByExternalID looks up an album that carries the ID as a non-primary
// alias in external_ids.
func (r *AlbumRepo) FindByExternalID(ctx context.Context, source, id string) (*domain.Album, error) {
	ext, err := marshalExternalIDs(map[string]string{sourceOrDefault(source): id})
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx,
		`SELECT `+albumCols+` FROM albums WHERE external_ids @> $1::jsonb`, ext)
	return scanAlbum(row)
}

func (r *AlbumRepo) FindAccessible(ctx context.Context, userID string) ([]domain.Album, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT `+albumColsPrefixed+`
		 FROM albums al
		 INNER JOIN track_albums ta ON ta.album_id = al.id
		 INNER JOIN tracks t ON t.id = ta.track_id
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
	ext, err := marshalExternalIDs(album.ExternalIDs)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE albums SET title=$1, artist_id=$2, external_id=$3, metadata_source=$4, external_ids=$5,
		 title_normalized=$6, country=$7, year=$8, genre=$9,
		 cover_image_id=$10, song_count=$11, duration=$12, updated_at=NOW()
		 WHERE id=$13`,
		album.Title, album.ArtistID, album.ExternalID, sourceOrDefault(album.MetadataSource), ext,
		utils.NormalizeName(album.Title), album.Country, album.Year, album.Genre,
		album.CoverImageID, album.SongCount, album.Duration, album.ID)
	return err
}
