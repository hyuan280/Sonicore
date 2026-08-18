package repository

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/core/domain"
)

func testJukebox() *domain.Jukebox {
	return &domain.Jukebox{
		ID:           "jb-001",
		Name:         "Living Room",
		DeviceID:     "hw:1,0",
		DeviceName:   "USB Audio",
		DeviceDriver: "alsa",
		Volume:       0.8,
		PlayMode:     "normal",
		Queue:        []string{"t-001", "t-002"},
		QueueIdx:     0,
		ShuffleOrder: []int{1, 0},
		ShuffleIdx:   1,
		PathMapping:  map[string]string{"/music": "/mnt/music"},
	}
}

func TestJukeboxRepoCreate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewJukeboxRepo(db)
	j := testJukebox()
	now := time.Now()

	queueJSON, _ := json.Marshal(j.Queue)
	shuffleJSON, _ := json.Marshal(j.ShuffleOrder)
	mappingJSON, _ := json.Marshal(j.PathMapping)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO jukeboxes`)).
		WithArgs(j.ID, j.Name, j.DeviceID, j.DeviceName, j.DeviceConfigID, j.DeviceDriver, j.Volume, j.PlayMode,
			queueJSON, j.QueueIdx, shuffleJSON, j.ShuffleIdx, mappingJSON).
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

	require.NoError(t, repo.Create(context.Background(), j))
	assert.Equal(t, now, j.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestJukeboxRepoGetByID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewJukeboxRepo(db)
	j := testJukebox()

	queueJSON, _ := json.Marshal(j.Queue)
	shuffleJSON, _ := json.Marshal(j.ShuffleOrder)
	mappingJSON, _ := json.Marshal(j.PathMapping)

	rows := sqlmock.NewRows([]string{"id", "name", "device_id", "device_name", "device_config_id", "device_driver", "volume", "play_mode", "queue", "queue_idx",
		"shuffle_order", "shuffle_idx", "path_mapping", "created_at", "updated_at"}).
		AddRow(j.ID, j.Name, j.DeviceID, j.DeviceName, j.DeviceConfigID, j.DeviceDriver, j.Volume, j.PlayMode,
			queueJSON, j.QueueIdx, shuffleJSON, j.ShuffleIdx, mappingJSON, time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`FROM jukeboxes WHERE id = $1`)).
		WithArgs("jb-001").
		WillReturnRows(rows)

	got, err := repo.GetByID(context.Background(), "jb-001")
	require.NoError(t, err)
	assert.Equal(t, j.Name, got.Name)
	assert.Equal(t, j.Queue, got.Queue)
	assert.Equal(t, j.ShuffleOrder, got.ShuffleOrder)
	assert.Equal(t, j.PathMapping, got.PathMapping)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestJukeboxRepoGetByIDEmptyMapping(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewJukeboxRepo(db)

	rows := sqlmock.NewRows([]string{"id", "name", "device_id", "device_name", "device_config_id", "device_driver", "volume", "play_mode", "queue", "queue_idx",
		"shuffle_order", "shuffle_idx", "path_mapping", "created_at", "updated_at"}).
		AddRow("jb-001", "X", "", "", "", "", 0.5, "normal", []byte("null"), 0, []byte("null"), 0, []byte("null"), time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`FROM jukeboxes WHERE id = $1`)).
		WithArgs("jb-001").
		WillReturnRows(rows)

	got, err := repo.GetByID(context.Background(), "jb-001")
	require.NoError(t, err)
	assert.Empty(t, got.Queue)
	assert.Empty(t, got.ShuffleOrder)
	assert.NotNil(t, got.PathMapping, "nil mapping should be normalized to empty map")
}

func TestJukeboxRepoList(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewJukeboxRepo(db)
	j := testJukebox()

	queueJSON, _ := json.Marshal(j.Queue)
	shuffleJSON, _ := json.Marshal(j.ShuffleOrder)
	mappingJSON, _ := json.Marshal(j.PathMapping)

	rows := sqlmock.NewRows([]string{"id", "name", "device_id", "device_name", "device_config_id", "device_driver", "volume", "play_mode", "queue", "queue_idx",
		"shuffle_order", "shuffle_idx", "path_mapping", "created_at", "updated_at"}).
		AddRow(j.ID, j.Name, j.DeviceID, j.DeviceName, j.DeviceConfigID, j.DeviceDriver, j.Volume, j.PlayMode,
			queueJSON, j.QueueIdx, shuffleJSON, j.ShuffleIdx, mappingJSON, time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`FROM jukeboxes ORDER BY created_at`)).
		WillReturnRows(rows)

	got, err := repo.List(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, j.Queue, got[0].Queue)
}

func TestJukeboxRepoUpdate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewJukeboxRepo(db)
	j := testJukebox()

	queueJSON, _ := json.Marshal(j.Queue)
	shuffleJSON, _ := json.Marshal(j.ShuffleOrder)
	mappingJSON, _ := json.Marshal(j.PathMapping)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE jukeboxes SET name=$1, device_id=$2, device_name=$3, device_config_id=$4, device_driver=$5, volume=$6, play_mode=$7,
		       queue=$8, queue_idx=$9, shuffle_order=$10, shuffle_idx=$11,
		       path_mapping=$12, updated_at=$13
		WHERE id=$14`)).
		WithArgs(j.Name, j.DeviceID, j.DeviceName, j.DeviceConfigID, j.DeviceDriver, j.Volume, j.PlayMode,
			queueJSON, j.QueueIdx, shuffleJSON, j.ShuffleIdx, mappingJSON, sqlmock.AnyArg(), j.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Update(context.Background(), j))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestJukeboxRepoSaveState(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewJukeboxRepo(db)
	j := testJukebox()

	queueJSON, _ := json.Marshal(j.Queue)
	shuffleJSON, _ := json.Marshal(j.ShuffleOrder)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE jukeboxes SET queue=$1, queue_idx=$2, shuffle_order=$3, shuffle_idx=$4,
		       play_mode=$5, volume=$6, updated_at=NOW()
		WHERE id=$7`)).
		WithArgs(queueJSON, j.QueueIdx, shuffleJSON, j.ShuffleIdx, j.PlayMode, j.Volume, j.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.SaveState(context.Background(), j.ID, j.Queue, j.QueueIdx, j.ShuffleOrder, j.ShuffleIdx, j.PlayMode, j.Volume))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestJukeboxRepoUpdateSettings(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewJukeboxRepo(db)
	mapping := map[string]string{"/a": "/b"}

	mappingJSON, _ := json.Marshal(mapping)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE jukeboxes SET path_mapping=$1, updated_at=NOW() WHERE id=$2`)).
		WithArgs(mappingJSON, "jb-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.UpdateSettings(context.Background(), "jb-001", mapping))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestJukeboxRepoDelete(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewJukeboxRepo(db)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM jukeboxes WHERE id = $1`)).
		WithArgs("jb-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Delete(context.Background(), "jb-001"))
	require.NoError(t, mock.ExpectationsWereMet())
}
