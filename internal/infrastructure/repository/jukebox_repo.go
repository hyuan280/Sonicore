package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/sonicore/server/internal/core/domain"
)

type JukeboxRepo struct {
	db *sql.DB
}

func NewJukeboxRepo(db *sql.DB) *JukeboxRepo {
	return &JukeboxRepo{db: db}
}

func (r *JukeboxRepo) Create(ctx context.Context, j *domain.Jukebox) error {
	queueJSON, _ := json.Marshal(j.Queue)
	shuffleJSON, _ := json.Marshal(j.ShuffleOrder)
	mappingJSON, _ := json.Marshal(j.PathMapping)

	return r.db.QueryRowContext(ctx, `
		INSERT INTO jukeboxes (id, name, device_id, device_name, device_config_id, device_driver, volume, play_mode, queue, queue_idx, shuffle_order, shuffle_idx, path_mapping)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING created_at, updated_at
	`, j.ID, j.Name, j.DeviceID, j.DeviceName, j.DeviceConfigID, j.DeviceDriver, j.Volume, j.PlayMode, queueJSON, j.QueueIdx, shuffleJSON, j.ShuffleIdx, mappingJSON).
		Scan(&j.CreatedAt, &j.UpdatedAt)
}

const jukeboxColumns = `id, name, device_id, device_name, device_config_id, device_driver, volume, play_mode, queue, queue_idx,
		       shuffle_order, shuffle_idx,
		       path_mapping, created_at, updated_at`

func (r *JukeboxRepo) List(ctx context.Context) ([]*domain.Jukebox, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+jukeboxColumns+` FROM jukeboxes ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*domain.Jukebox
	for rows.Next() {
		j, err := scanJukebox(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, j)
	}
	return list, rows.Err()
}

func (r *JukeboxRepo) GetByID(ctx context.Context, id string) (*domain.Jukebox, error) {
	j := &domain.Jukebox{}
	var queueJSON, shuffleJSON, mappingJSON []byte
	err := r.db.QueryRowContext(ctx, `SELECT `+jukeboxColumns+` FROM jukeboxes WHERE id = $1`, id).
		Scan(&j.ID, &j.Name, &j.DeviceID, &j.DeviceName, &j.DeviceConfigID, &j.DeviceDriver, &j.Volume, &j.PlayMode,
			&queueJSON, &j.QueueIdx, &shuffleJSON, &j.ShuffleIdx,
			&mappingJSON, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(queueJSON, &j.Queue)
	json.Unmarshal(shuffleJSON, &j.ShuffleOrder)
	json.Unmarshal(mappingJSON, &j.PathMapping)
	if j.PathMapping == nil {
		j.PathMapping = make(map[string]string)
	}
	return j, nil
}

func (r *JukeboxRepo) Update(ctx context.Context, j *domain.Jukebox) error {
	queueJSON, _ := json.Marshal(j.Queue)
	shuffleJSON, _ := json.Marshal(j.ShuffleOrder)
	mappingJSON, _ := json.Marshal(j.PathMapping)
	j.UpdatedAt = time.Now()

	_, err := r.db.ExecContext(ctx, `
		UPDATE jukeboxes SET name=$1, device_id=$2, device_name=$3, device_config_id=$4, device_driver=$5, volume=$6, play_mode=$7,
		       queue=$8, queue_idx=$9, shuffle_order=$10, shuffle_idx=$11,
		       path_mapping=$12, updated_at=$13
		WHERE id=$14
	`, j.Name, j.DeviceID, j.DeviceName, j.DeviceConfigID, j.DeviceDriver, j.Volume, j.PlayMode,
		queueJSON, j.QueueIdx, shuffleJSON, j.ShuffleIdx, mappingJSON, j.UpdatedAt, j.ID)
	return err
}

func (r *JukeboxRepo) UpdateSettings(ctx context.Context, id string, mapping map[string]string) error {
	mappingJSON, _ := json.Marshal(mapping)
	_, err := r.db.ExecContext(ctx, `
		UPDATE jukeboxes SET path_mapping=$1, updated_at=NOW() WHERE id=$2
	`, mappingJSON, id)
	return err
}

func (r *JukeboxRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM jukeboxes WHERE id = $1`, id)
	return err
}

func (r *JukeboxRepo) SaveState(ctx context.Context, id string, queue []string, queueIdx int, shuffleOrder []int, shuffleIdx int, playMode string, volume float64) error {
	queueJSON, _ := json.Marshal(queue)
	shuffleJSON, _ := json.Marshal(shuffleOrder)
	_, err := r.db.ExecContext(ctx, `
		UPDATE jukeboxes SET queue=$1, queue_idx=$2, shuffle_order=$3, shuffle_idx=$4,
		       play_mode=$5, volume=$6, updated_at=NOW()
		WHERE id=$7
	`, queueJSON, queueIdx, shuffleJSON, shuffleIdx, playMode, volume, id)
	return err
}

func scanJukebox(rows interface{ Scan(...interface{}) error }) (*domain.Jukebox, error) {
	j := &domain.Jukebox{}
	var queueJSON, shuffleJSON, mappingJSON []byte
	err := rows.Scan(&j.ID, &j.Name, &j.DeviceID, &j.DeviceName, &j.DeviceConfigID, &j.DeviceDriver, &j.Volume, &j.PlayMode,
		&queueJSON, &j.QueueIdx, &shuffleJSON, &j.ShuffleIdx,
		&mappingJSON, &j.CreatedAt, &j.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(queueJSON, &j.Queue)
	json.Unmarshal(shuffleJSON, &j.ShuffleOrder)
	json.Unmarshal(mappingJSON, &j.PathMapping)
	if j.PathMapping == nil {
		j.PathMapping = make(map[string]string)
	}
	return j, nil
}
