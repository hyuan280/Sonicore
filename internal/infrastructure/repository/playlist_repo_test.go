package repository

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testPlaylist() *domain.Playlist {
	now := time.Date(2024, 4, 1, 15, 30, 0, 0, time.UTC)
	return &domain.Playlist{
		ID:       "pl-001",
		Name:     "Road Trip",
		OwnerID:  "u-001",
		IsPublic: true,
		TrackIDs: []string{"t-001", "t-002"},
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestPlaylistRepoCreate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewPlaylistRepo(db)
	p := testPlaylist()

	trackIDs, _ := json.Marshal(p.TrackIDs)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO playlists (id, name, owner_id, is_public, track_ids, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`)).
		WithArgs(p.ID, p.Name, p.OwnerID, p.IsPublic, trackIDs, p.CreatedAt, p.UpdatedAt).
		WillReturnResult(sqlmock.NewResult(1, 1))

	require.NoError(t, repo.Create(context.Background(), p))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPlaylistRepoFindByID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewPlaylistRepo(db)
	p := testPlaylist()

	trackIDs, _ := json.Marshal(p.TrackIDs)
	rows := sqlmock.NewRows([]string{"id", "name", "owner_id", "is_public", "track_ids", "created_at", "updated_at"}).
		AddRow(p.ID, p.Name, p.OwnerID, p.IsPublic, trackIDs, p.CreatedAt, p.UpdatedAt)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM playlists WHERE id = $1`)).
		WithArgs("pl-001").
		WillReturnRows(rows)

	got, err := repo.FindByID(context.Background(), "pl-001")
	require.NoError(t, err)
	assert.Equal(t, p, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPlaylistRepoFindByIDNullTrackIDs(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewPlaylistRepo(db)

	rows := sqlmock.NewRows([]string{"id", "name", "owner_id", "is_public", "track_ids", "created_at", "updated_at"}).
		AddRow("pl-001", "Empty", "u-001", false, nil, time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`FROM playlists WHERE id = $1`)).
		WithArgs("pl-001").
		WillReturnRows(rows)

	got, err := repo.FindByID(context.Background(), "pl-001")
	require.NoError(t, err)
	assert.Nil(t, got.TrackIDs)
}

func TestPlaylistRepoFindByUserID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewPlaylistRepo(db)
	p1, p2 := testPlaylist(), testPlaylist()
	p2.ID, p2.Name = "pl-002", "Gym Mix"

	trackIDs, _ := json.Marshal(p1.TrackIDs)
	rows := sqlmock.NewRows([]string{"id", "name", "owner_id", "is_public", "track_ids", "created_at", "updated_at"}).
		AddRow(p1.ID, p1.Name, p1.OwnerID, p1.IsPublic, trackIDs, p1.CreatedAt, p1.UpdatedAt).
		AddRow(p2.ID, p2.Name, p2.OwnerID, p2.IsPublic, trackIDs, p2.CreatedAt, p2.UpdatedAt)

	mock.ExpectQuery(regexp.QuoteMeta(`FROM playlists WHERE owner_id = $1 ORDER BY name`)).
		WithArgs("u-001").
		WillReturnRows(rows)

	got, err := repo.FindByUserID(context.Background(), "u-001")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"t-001", "t-002"}, got[0].TrackIDs)
}

func TestPlaylistRepoUpdate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewPlaylistRepo(db)
	p := testPlaylist()
	p.TrackIDs = []string{"t-003"}

	trackIDs, _ := json.Marshal(p.TrackIDs)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE playlists SET name=$1, is_public=$2, track_ids=$3, updated_at=NOW()
		 WHERE id=$4`)).
		WithArgs(p.Name, p.IsPublic, trackIDs, p.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Update(context.Background(), p))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPlaylistRepoDelete(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewPlaylistRepo(db)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM playlists WHERE id = $1`)).
		WithArgs("pl-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Delete(context.Background(), "pl-001"))
	require.NoError(t, mock.ExpectationsWereMet())
}
