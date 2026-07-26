package repository

import (
	"context"
	"database/sql"

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
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
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

func (r *ImageRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM images WHERE id = $1`, id)
	return err
}

func scanImage(scanner interface{ Scan(dest ...interface{}) error }) (*domain.Image, error) {
	var img domain.Image
	err := scanner.Scan(&img.ID, &img.LibraryID, &img.OwnerType, &img.OwnerID,
		&img.Source, &img.Path, &img.Format, &img.Width, &img.Height,
		&img.Size, &img.Hash, &img.Variants, &img.CreatedAt, &img.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &img, nil
}
