package repository

import (
	"context"
	"database/sql"

	"github.com/lib/pq"

	"github.com/sonicore/server/internal/core/domain"
)

type ImageRepo struct {
	db *sql.DB
}

func NewImageRepo(db *sql.DB) *ImageRepo {
	return &ImageRepo{db: db}
}

func (r *ImageRepo) Create(ctx context.Context, img *domain.Image) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO images (id, library_id, owner_type, owner_id, source, path,
		 format, width, height, size, hash, variants, created_at, updated_at)
		 VALUES ($1,NULLIF($2,''),$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		img.ID, img.LibraryID, img.OwnerType, img.OwnerID, img.Source, img.Path,
		img.Format, img.Width, img.Height, img.Size, img.Hash, img.Variants,
		img.CreatedAt, img.UpdatedAt)
	return err
}

func (r *ImageRepo) FindByID(ctx context.Context, id string) (*domain.Image, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, owner_type, owner_id, source, path,
		 format, width, height, size, hash, variants, created_at, updated_at
		 FROM images WHERE id = $1`, id)
	return scanImage(row)
}

func (r *ImageRepo) FindByOwner(ctx context.Context, ownerType, ownerID string) (*domain.Image, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, library_id, owner_type, owner_id, source, path,
		 format, width, height, size, hash, variants, created_at, updated_at
		 FROM images WHERE owner_type = $1 AND owner_id = $2`,
		ownerType, ownerID)
	return scanImage(row)
}

// FindByPath lists image rows of an owner type that reference the given
// path (used to find album rows pointing at a track's original).
func (r *ImageRepo) FindByPath(ctx context.Context, ownerType, path string) ([]domain.Image, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, owner_type, owner_id, source, path,
		 format, width, height, size, hash, variants, created_at, updated_at
		 FROM images WHERE owner_type = $1 AND path = $2`, ownerType, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Image
	for rows.Next() {
		img, err := scanImage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *img)
	}
	return out, rows.Err()
}

// FindOrphans returns image rows whose owning entity no longer exists
// (e.g. a track deleted through a path that did not clean its cover, a crash
// between cleanup steps, or manual database edits). owner_id has no foreign
// key to the entity tables, so orphaned rows cannot be removed by a CASCADE.
func (r *ImageRepo) FindOrphans(ctx context.Context) ([]domain.Image, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, library_id, owner_type, owner_id, source, path,
		 format, width, height, size, hash, variants, created_at, updated_at
		 FROM images
		 WHERE (owner_type = 'track' AND NOT EXISTS (SELECT 1 FROM tracks t WHERE t.id = images.owner_id))
		    OR (owner_type = 'album' AND NOT EXISTS (SELECT 1 FROM albums a WHERE a.id = images.owner_id))
		    OR (owner_type = 'artist' AND NOT EXISTS (SELECT 1 FROM artists ar WHERE ar.id = images.owner_id))`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Image
	for rows.Next() {
		img, err := scanImage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *img)
	}
	return out, rows.Err()
}

func (r *ImageRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM images WHERE id = $1`, id)
	return err
}

// DeleteByOwner removes every image record owned by the given entity
// (owner_type: track/album/artist).
func (r *ImageRepo) DeleteByOwner(ctx context.Context, ownerType, ownerID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM images WHERE owner_type = $1 AND owner_id = $2`, ownerType, ownerID)
	return err
}

// SharedPaths reports which of the given paths are referenced by at least one
// image row outside the excluded set. Used by the orphan sweep so a shared
// file (e.g. an album cover pointing at a live track's original) is never
// removed out from under a still-valid row. excludedIDs is the orphan snapshot:
// rows belonging to those orphans are about to be deleted by the same sweep and
// must not count as a live reference — a path referenced only by snapshot
// orphans is absent from the result and therefore removable (the sweep's
// idempotent os.Remove cleans it up once, regardless of how many orphans
// reference it).
func (r *ImageRepo) SharedPaths(ctx context.Context, paths, excludedIDs []string) (map[string]struct{}, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT DISTINCT path FROM images WHERE path = ANY($1) AND id != ALL($2)`,
		pq.Array(paths), pq.Array(excludedIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	shared := make(map[string]struct{})
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		shared[p] = struct{}{}
	}
	return shared, rows.Err()
}

// Update refreshes the mutable metadata of an image row (used when a track
// re-extraction changes the bytes behind a shared album cover).
func (r *ImageRepo) Update(ctx context.Context, img *domain.Image) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE images SET format=$1, width=$2, height=$3, size=$4, hash=$5, variants=$6, updated_at=NOW()
		 WHERE id=$7`,
		img.Format, img.Width, img.Height, img.Size, img.Hash, img.Variants, img.ID)
	return err
}

func scanImage(scanner interface{ Scan(dest ...interface{}) error }) (*domain.Image, error) {
	var img domain.Image
	var libID sql.NullString
	err := scanner.Scan(&img.ID, &libID, &img.OwnerType, &img.OwnerID,
		&img.Source, &img.Path, &img.Format, &img.Width, &img.Height,
		&img.Size, &img.Hash, &img.Variants, &img.CreatedAt, &img.UpdatedAt)
	if err != nil {
		return nil, err
	}
	img.LibraryID = libID.String
	return &img, nil
}
