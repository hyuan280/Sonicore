package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/core/domain"
)

func testAudioDevice() *domain.AudioDeviceConfig {
	return &domain.AudioDeviceConfig{
		ID:         "dev-001",
		Name:       "USB DAC",
		DeviceType: "local",
		DeviceID:   "hw:1,0",
		Driver:     "alsa",
		Config:     map[string]string{"mixer": "PCM"},
	}
}

func TestAudioDeviceRepoCreate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAudioDeviceRepo(db)
	d := testAudioDevice()
	now := time.Now()

	cfgJSON, _ := json.Marshal(d.Config)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM audio_devices WHERE device_id=$1 AND driver=$2`)).
		WithArgs(d.DeviceID, d.Driver).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM audio_devices WHERE name=$1`)).
		WithArgs(d.Name).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO audio_devices (id, name, device_type, device_id, driver, config)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at, updated_at`)).
		WithArgs(d.ID, d.Name, d.DeviceType, d.DeviceID, d.Driver, cfgJSON).
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

	require.NoError(t, repo.Create(context.Background(), d))
	assert.Equal(t, now, d.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAudioDeviceRepoCreateDuplicateDevice(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAudioDeviceRepo(db)
	d := testAudioDevice()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM audio_devices WHERE device_id=$1 AND driver=$2`)).
		WithArgs(d.DeviceID, d.Driver).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	err := repo.Create(context.Background(), d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestAudioDeviceRepoCreateDuplicateName(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAudioDeviceRepo(db)
	d := testAudioDevice()

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM audio_devices WHERE device_id=$1 AND driver=$2`)).
		WithArgs(d.DeviceID, d.Driver).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM audio_devices WHERE name=$1`)).
		WithArgs(d.Name).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	err := repo.Create(context.Background(), d)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}

func TestAudioDeviceRepoList(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAudioDeviceRepo(db)
	d := testAudioDevice()

	cfgJSON, _ := json.Marshal(d.Config)
	rows := sqlmock.NewRows([]string{"id", "name", "device_type", "device_id", "driver", "config", "created_at", "updated_at"}).
		AddRow(d.ID, d.Name, d.DeviceType, d.DeviceID, d.Driver, cfgJSON, time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`FROM audio_devices ad ORDER BY ad.created_at`)).
		WillReturnRows(rows)

	got, err := repo.List(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, d.Config, got[0].Config)
}

func TestAudioDeviceRepoListAvailable(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAudioDeviceRepo(db)
	d := testAudioDevice()

	cfgJSON, _ := json.Marshal(d.Config)
	rows := sqlmock.NewRows([]string{"id", "name", "device_type", "device_id", "driver", "config", "created_at", "updated_at"}).
		AddRow(d.ID, d.Name, d.DeviceType, d.DeviceID, d.Driver, cfgJSON, time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`WHERE j.id IS NULL`)).
		WillReturnRows(rows)

	got, err := repo.ListAvailable(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestAudioDeviceRepoGetByID(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAudioDeviceRepo(db)
	d := testAudioDevice()

	cfgJSON, _ := json.Marshal(d.Config)
	rows := sqlmock.NewRows([]string{"id", "name", "device_type", "device_id", "driver", "config", "created_at", "updated_at"}).
		AddRow(d.ID, d.Name, d.DeviceType, d.DeviceID, d.Driver, cfgJSON, time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`FROM audio_devices WHERE id = $1`)).
		WithArgs("dev-001").
		WillReturnRows(rows)

	got, err := repo.GetByID(context.Background(), "dev-001")
	require.NoError(t, err)
	assert.Equal(t, d.Name, got.Name)
	assert.Equal(t, d.Config, got.Config)
}

func TestAudioDeviceRepoGetByIDEmptyConfig(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAudioDeviceRepo(db)

	rows := sqlmock.NewRows([]string{"id", "name", "device_type", "device_id", "driver", "config", "created_at", "updated_at"}).
		AddRow("dev-001", "X", "local", "", "alsa", []byte("null"), time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`FROM audio_devices WHERE id = $1`)).
		WithArgs("dev-001").
		WillReturnRows(rows)

	got, err := repo.GetByID(context.Background(), "dev-001")
	require.NoError(t, err)
	assert.NotNil(t, got.Config, "nil config should be normalized to empty map")
}

func TestAudioDeviceRepoUpdate(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAudioDeviceRepo(db)
	d := testAudioDevice()

	cfgJSON, _ := json.Marshal(d.Config)
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE audio_devices SET name=$1, device_type=$2, device_id=$3, driver=$4, config=$5, updated_at=$6
		 WHERE id=$7`)).
		WithArgs(d.Name, d.DeviceType, d.DeviceID, d.Driver, cfgJSON, sqlmock.AnyArg(), d.ID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Update(context.Background(), d))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAudioDeviceRepoDelete(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAudioDeviceRepo(db)

	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM audio_devices WHERE id = $1`)).
		WithArgs("dev-001").
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Delete(context.Background(), "dev-001"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAudioDeviceRepoGetBoundJukebox(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAudioDeviceRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id FROM jukeboxes WHERE device_config_id = $1`)).
		WithArgs("dev-001").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("jb-001"))

	id, err := repo.GetBoundJukebox(context.Background(), "dev-001")
	require.NoError(t, err)
	assert.Equal(t, "jb-001", id)
}

func TestAudioDeviceRepoGetBoundJukeboxUnbound(t *testing.T) {
	db, mock := newMockDB(t)
	repo := NewAudioDeviceRepo(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id FROM jukeboxes WHERE device_config_id = $1`)).
		WithArgs("dev-001").
		WillReturnError(sql.ErrNoRows)

	id, err := repo.GetBoundJukebox(context.Background(), "dev-001")
	require.NoError(t, err)
	assert.Equal(t, "", id)
}
