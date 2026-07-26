package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sonicore/server/internal/core/domain"
)

type AudioDeviceRepo struct {
	db *sql.DB
}

func NewAudioDeviceRepo(db *sql.DB) *AudioDeviceRepo {
	return &AudioDeviceRepo{db: db}
}

func (r *AudioDeviceRepo) Create(ctx context.Context, d *domain.AudioDeviceConfig) error {
	cfgJSON, _ := json.Marshal(d.Config)

	// check if same device_id + driver already exists
	var count int
	r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audio_devices WHERE device_id=$1 AND driver=$2`,
		d.DeviceID, d.Driver).Scan(&count)
	if count > 0 {
		return fmt.Errorf("device %q with driver %q already exists", d.DeviceID, d.Driver)
	}

	// check if same name already exists
	r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audio_devices WHERE name=$1`,
		d.Name).Scan(&count)
	if count > 0 {
		return fmt.Errorf("device name %q already exists", d.Name)
	}

	return r.db.QueryRowContext(ctx, `
		INSERT INTO audio_devices (id, name, device_type, device_id, driver, config)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING created_at, updated_at
	`, d.ID, d.Name, d.DeviceType, d.DeviceID, d.Driver, cfgJSON).
		Scan(&d.CreatedAt, &d.UpdatedAt)
}

func (r *AudioDeviceRepo) List(ctx context.Context) ([]*domain.AudioDeviceConfig, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT ad.id, ad.name, ad.device_type, ad.device_id, ad.driver, ad.config,
		        ad.created_at, ad.updated_at
		 FROM audio_devices ad ORDER BY ad.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*domain.AudioDeviceConfig
	for rows.Next() {
		d, err := scanAudioDevice(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

func (r *AudioDeviceRepo) ListAvailable(ctx context.Context) ([]*domain.AudioDeviceConfig, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT ad.id, ad.name, ad.device_type, ad.device_id, ad.driver, ad.config,
		        ad.created_at, ad.updated_at
		 FROM audio_devices ad
		 LEFT JOIN jukeboxes j ON j.device_config_id = ad.id
		 WHERE j.id IS NULL
		 ORDER BY ad.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*domain.AudioDeviceConfig
	for rows.Next() {
		d, err := scanAudioDevice(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

func (r *AudioDeviceRepo) GetByID(ctx context.Context, id string) (*domain.AudioDeviceConfig, error) {
	d := &domain.AudioDeviceConfig{}
	var cfgJSON []byte
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, device_type, device_id, driver, config, created_at, updated_at
		 FROM audio_devices WHERE id = $1`, id).
		Scan(&d.ID, &d.Name, &d.DeviceType, &d.DeviceID, &d.Driver, &cfgJSON, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(cfgJSON, &d.Config)
	if d.Config == nil {
		d.Config = make(map[string]string)
	}
	return d, nil
}

func (r *AudioDeviceRepo) Update(ctx context.Context, d *domain.AudioDeviceConfig) error {
	cfgJSON, _ := json.Marshal(d.Config)
	d.UpdatedAt = time.Now()
	_, err := r.db.ExecContext(ctx,
		`UPDATE audio_devices SET name=$1, device_type=$2, device_id=$3, driver=$4, config=$5, updated_at=$6
		 WHERE id=$7`,
		d.Name, d.DeviceType, d.DeviceID, d.Driver, cfgJSON, d.UpdatedAt, d.ID)
	return err
}

func (r *AudioDeviceRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM audio_devices WHERE id = $1`, id)
	return err
}

func (r *AudioDeviceRepo) GetBoundJukebox(ctx context.Context, deviceID string) (string, error) {
	var jukeboxID string
	err := r.db.QueryRowContext(ctx,
		`SELECT id FROM jukeboxes WHERE device_config_id = $1`, deviceID).
		Scan(&jukeboxID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return jukeboxID, err
}

func scanAudioDevice(rows interface{ Scan(...interface{}) error }) (*domain.AudioDeviceConfig, error) {
	d := &domain.AudioDeviceConfig{}
	var cfgJSON []byte
	err := rows.Scan(&d.ID, &d.Name, &d.DeviceType, &d.DeviceID, &d.Driver, &cfgJSON, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	json.Unmarshal(cfgJSON, &d.Config)
	if d.Config == nil {
		d.Config = make(map[string]string)
	}
	return d, nil
}
