package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/pkg/utils"
)

type ArtistRepo struct {
	db *sql.DB
}

func NewArtistRepo(db *sql.DB) *ArtistRepo {
	return &ArtistRepo{db: db}
}

// artistCols is the canonical column list for scanArtist. Keep in sync with
// the scan order in scanArtist.
const artistCols = `id, name, sort_name, mbid, metadata_source, external_ids, country, biography, cover_image_id, created_at, updated_at`

// artistColsPrefixed is artistCols qualified with the a. alias for JOIN queries.
const artistColsPrefixed = `a.id, a.name, a.sort_name, a.mbid, a.metadata_source, a.external_ids, a.country, a.biography, a.cover_image_id, a.created_at, a.updated_at`

func scanArtist(scanner interface{ Scan(dest ...interface{}) error }) (*domain.Artist, error) {
	var a domain.Artist
	var coverID sql.NullString
	var ext []byte
	var roles string
	err := scanner.Scan(&a.ID, &a.Name, &a.SortName,
		&a.MBID, &a.MetadataSource, &ext, &a.Country, &a.Biography, &coverID,
		&a.CreatedAt, &a.UpdatedAt, &a.TrackCount, &roles)
	if err != nil {
		return nil, err
	}
	if len(ext) > 0 {
		var m map[string]string
		if err := json.Unmarshal(ext, &m); err != nil {
			return nil, fmt.Errorf("decode external_ids for artist %s: %w", a.ID, err)
		}
		a.ExternalIDs = m
	}
	if coverID.Valid {
		a.CoverImageID = &coverID.String
	}
	if roles != "" {
		a.Roles = strings.Split(roles, ",")
	}
	return &a, nil
}

func (r *ArtistRepo) BatchCreate(ctx context.Context, artists []domain.Artist) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// On a conflicting id, the incoming record wins for name and identity
	// fields (metadata_source/external_ids). Callers of BatchCreate supply
	// complete entities (scanner/entity-resolver), so a conflict refresh is
	// the intended upsert contract; partial-object callers should use Update.
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO artists (id, name, sort_name, mbid, metadata_source, external_ids, name_normalized, country, biography, cover_image_id, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		 ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, name_normalized=EXCLUDED.name_normalized,
		 metadata_source=EXCLUDED.metadata_source, external_ids=EXCLUDED.external_ids, updated_at=NOW()`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, a := range artists {
		ext, err := marshalExternalIDs(a.ExternalIDs)
		if err != nil {
			return err
		}
		_, err = stmt.ExecContext(ctx, a.ID, a.Name, a.SortName,
			a.MBID, sourceOrDefault(a.MetadataSource), ext, utils.NormalizeName(a.Name),
			a.Country, a.Biography, a.CoverImageID, a.CreatedAt, a.UpdatedAt)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *ArtistRepo) FindByID(ctx context.Context, id string) (*domain.Artist, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+artistCols+`, 0 AS track_count,
		 COALESCE((SELECT string_agg(DISTINCT ta.role, ',' ORDER BY ta.role) FROM track_artists ta WHERE ta.artist_id = $1), '') AS roles
		 FROM artists WHERE id = $1`, id)
	return scanArtist(row)
}

func (r *ArtistRepo) FindByLibraryID(ctx context.Context, libraryID string) ([]domain.Artist, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT `+artistColsPrefixed+`, 0 AS track_count,
		 '' AS roles
		 FROM artists a
		 INNER JOIN track_artists ta ON ta.artist_id = a.id
		 INNER JOIN tracks t ON t.id = ta.track_id
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
		`SELECT DISTINCT `+artistColsPrefixed+`,
		 (SELECT COUNT(*) FROM tracks
		  INNER JOIN library_members lm2 ON lm2.library_id = tracks.library_id
		  INNER JOIN track_artists ta2 ON ta2.track_id = tracks.id
		  WHERE ta2.artist_id = a.id AND lm2.user_id = $1) AS track_count,
		 (SELECT string_agg(DISTINCT ta3.role, ',' ORDER BY ta3.role)
		  FROM track_artists ta3
		  INNER JOIN tracks t3 ON t3.id = ta3.track_id
		  INNER JOIN library_members lm3 ON lm3.library_id = t3.library_id
		  WHERE ta3.artist_id = a.id AND lm3.user_id = $1) AS roles
		 FROM artists a
		 INNER JOIN track_artists ta ON ta.artist_id = a.id
		 INNER JOIN tracks t ON t.id = ta.track_id
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

// FindByMBID looks up an artist by its primary external ID, restricted to
// the given source (defaults to musicbrainz for backward compatibility).
func (r *ArtistRepo) FindByMBID(ctx context.Context, mbid string) (*domain.Artist, error) {
	return r.FindBySourceAndID(ctx, "musicbrainz", mbid)
}

// FindBySourceAndID looks up an artist by its primary external ID within a
// source (metadata_source + mbid).
func (r *ArtistRepo) FindBySourceAndID(ctx context.Context, source, id string) (*domain.Artist, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+artistCols+`, 0 AS track_count, '' AS roles
		 FROM artists WHERE metadata_source = $1 AND mbid = $2`, sourceOrDefault(source), id)
	return scanArtist(row)
}

// FindByExternalID looks up an artist that carries the ID as a non-primary
// alias in external_ids (e.g. {"netease":"6452"}).
func (r *ArtistRepo) FindByExternalID(ctx context.Context, source, id string) (*domain.Artist, error) {
	ext, err := marshalExternalIDs(map[string]string{sourceOrDefault(source): id})
	if err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx,
		`SELECT `+artistCols+`, 0 AS track_count, '' AS roles
		 FROM artists WHERE external_ids @> $1::jsonb`, ext)
	return scanArtist(row)
}

// FindByNameNormalized looks up an artist by its canonical name (NFKC,
// lowercase, punctuation stripped).
func (r *ArtistRepo) FindByNameNormalized(ctx context.Context, name string) (*domain.Artist, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+artistCols+`, 0 AS track_count, '' AS roles
		 FROM artists WHERE name_normalized = $1`, utils.NormalizeName(name))
	return scanArtist(row)
}

func (r *ArtistRepo) FindByNameAndLibrary(ctx context.Context, name, libraryID string) (*domain.Artist, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+artistCols+`, 0 AS track_count, '' AS roles
		 FROM artists WHERE name = $1`, name)
	return scanArtist(row)
}

func (r *ArtistRepo) FindByName(ctx context.Context, name string) (*domain.Artist, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT `+artistCols+`, 0 AS track_count, '' AS roles
		 FROM artists WHERE name = $1`, name)
	return scanArtist(row)
}

func (r *ArtistRepo) Update(ctx context.Context, artist *domain.Artist) error {
	ext, err := marshalExternalIDs(artist.ExternalIDs)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx,
		`UPDATE artists SET name=$1, sort_name=$2, mbid=$3, metadata_source=$4, external_ids=$5,
		 name_normalized=$6, country=$7, biography=$8, cover_image_id=$9, updated_at=NOW()
		 WHERE id=$10`,
		artist.Name, artist.SortName, artist.MBID, sourceOrDefault(artist.MetadataSource), ext,
		utils.NormalizeName(artist.Name), artist.Country, artist.Biography, artist.CoverImageID, artist.ID)
	return err
}
