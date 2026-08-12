package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testImage() *domain.Image {
	now := time.Date(2024, 8, 1, 12, 0, 0, 0, time.UTC)
	return &domain.Image{
		ID:        "img-001",
		LibraryID: "lib-001",
		OwnerType: "album",
		OwnerID:   "alb-001",
		Source:    "embedded",
		Path:      "/images/img-001.png",
		Format:    "png",
		Width:     500,
		Height:    500,
		Size:      2048,
		Hash:      "abc123",
		Variants: domain.ImageVariants{
			{Path: "/images/img-001_300.png", Width: 300, Height: 300, Size: 1024},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func imageRows(img *domain.Image) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "library_id", "owner_type", "owner_id", "source", "path",
		"format", "width", "height", "size", "hash", "variants", "created_at", "updated_at"}).
		AddRow(img.ID, img.LibraryID, img.OwnerType, img.OwnerID, img.Source, img.Path,
			img.Format, img.Width, img.Height, img.Size, img.Hash, img.Variants, img.CreatedAt, img.UpdatedAt)
}

func TestImageRepoCreate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewImageRepo(db)
	img := testImage()

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO images (id, library_id, owner_type, owner_id, source, path,
		 format, width, height, size, hash, variants, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`)).
		WithArgs(img.ID, img.LibraryID, img.OwnerType, img.OwnerID, img.Source, img.Path,
			img.Format, img.Width, img.Height, img.Size, img.Hash, img.Variants, img.CreatedAt, img.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, repo.Create(context.Background(), img))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageRepoFindByID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewImageRepo(db)
	img := testImage()

	mock.ExpectQuery(regexp.QuoteMeta(`FROM images WHERE id = $1`)).
		WithArgs("img-001").
		WillReturnRows(imageRows(img))

	got, err := repo.FindByID(context.Background(), "img-001")
	require.NoError(t, err)
	assert.Equal(t, img, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestImageRepoFindByOwner(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewImageRepo(db)
	img := testImage()

	mock.ExpectQuery(regexp.QuoteMeta(`FROM images WHERE owner_type = $1 AND owner_id = $2`)).
		WithArgs("album", "alb-001").
		WillReturnRows(imageRows(img))

	got, err := repo.FindByOwner(context.Background(), "album", "alb-001")
	require.NoError(t, err)
	assert.Equal(t, img.ID, got.ID)
}

func TestImageRepoFindByIDWithNilVariants(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewImageRepo(db)

	rows := sqlmock.NewRows([]string{"id", "library_id", "owner_type", "owner_id", "source", "path",
		"format", "width", "height", "size", "hash", "variants", "created_at", "updated_at"}).
		AddRow("img-001", "", "album", "alb-1", "embedded", "/a.png", "png", 0, 0, int64(0), "h", nil, time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`FROM images WHERE id = $1`)).
		WithArgs("img-001").
		WillReturnRows(rows)

	got, err := repo.FindByID(context.Background(), "img-001")
	require.NoError(t, err)
	assert.Empty(t, got.Variants)
}

func TestImageRepoDelete(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewImageRepo(db)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM images WHERE id = $1`)).
		WithArgs("img-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Delete(context.Background(), "img-001"))
	require.NoError(t, mock.ExpectationsWereMet())
}
