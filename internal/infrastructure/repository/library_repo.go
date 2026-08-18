package repository

import (
	"context"
	"database/sql"

	"github.com/sonicore/server/internal/core/domain"
)

type LibraryRepo struct {
	db *sql.DB
}

func NewLibraryRepo(db *sql.DB) *LibraryRepo {
	return &LibraryRepo{db: db}
}

func scanLibrary(scanner interface {
	Scan(dest ...interface{}) error
}) (*domain.Library, error) {
	var l domain.Library
	err := scanner.Scan(&l.ID, &l.Name, &l.Path, &l.OwnerID,
		&l.MetadataStorageMode, &l.ScanInterval, &l.LastScannedAt,
		&l.LastScanErrors,
		&l.TrackCount, &l.Duration, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *LibraryRepo) Create(ctx context.Context, lib *domain.Library) error {
	query := `INSERT INTO libraries (id, name, path, owner_id, metadata_storage_mode, scan_interval,
		track_count, duration, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.db.ExecContext(ctx, query,
		lib.ID, lib.Name, lib.Path, lib.OwnerID,
		lib.MetadataStorageMode, lib.ScanInterval,
		lib.TrackCount, lib.Duration, lib.CreatedAt, lib.UpdatedAt)
	return err
}

func (r *LibraryRepo) FindByID(ctx context.Context, id string) (*domain.Library, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE id = $1`, id)
	return scanLibrary(row)
}

func (r *LibraryRepo) FindByOwnerID(ctx context.Context, ownerID string) ([]domain.Library, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, path, owner_id, metadata_storage_mode, scan_interval,
		 last_scanned_at, last_scan_errors, track_count, duration, created_at, updated_at
		 FROM libraries WHERE owner_id = $1 ORDER BY name`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var libs []domain.Library
	for rows.Next() {
		lib, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		libs = append(libs, *lib)
	}
	return libs, rows.Err()
}

func (r *LibraryRepo) FindByUserID(ctx context.Context, userID string) ([]domain.Library, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT l.id, l.name, l.path, l.owner_id, l.metadata_storage_mode, l.scan_interval,
		 l.last_scanned_at, l.last_scan_errors, l.track_count, l.duration, l.created_at, l.updated_at
		 FROM libraries l
		 JOIN library_members lm ON lm.library_id = l.id
		 WHERE lm.user_id = $1
		 ORDER BY l.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var libs []domain.Library
	for rows.Next() {
		lib, err := scanLibrary(rows)
		if err != nil {
			return nil, err
		}
		libs = append(libs, *lib)
	}
	return libs, rows.Err()
}

func (r *LibraryRepo) AddMember(ctx context.Context, member *domain.LibraryMember) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO library_members (library_id, user_id, role, joined_at)
		 VALUES ($1, $2, $3, $4)`,
		member.LibraryID, member.UserID, member.Role, member.JoinedAt)
	return err
}

func (r *LibraryRepo) RemoveMember(ctx context.Context, libraryID, userID string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM library_members WHERE library_id = $1 AND user_id = $2`,
		libraryID, userID)
	return err
}

func (r *LibraryRepo) UpdateMemberRole(ctx context.Context, libraryID, userID, role string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE library_members SET role = $1 WHERE library_id = $2 AND user_id = $3`,
		role, libraryID, userID)
	return err
}

func (r *LibraryRepo) GetMembers(ctx context.Context, libraryID string) ([]domain.LibraryMember, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT library_id, user_id, role, joined_at FROM library_members WHERE library_id = $1`,
		libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []domain.LibraryMember
	for rows.Next() {
		var m domain.LibraryMember
		if err := rows.Scan(&m.LibraryID, &m.UserID, &m.Role, &m.JoinedAt); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}

func (r *LibraryRepo) UpdateStats(ctx context.Context, lib *domain.Library) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE libraries SET track_count=$2, duration=$3, last_scanned_at=$4, last_scan_errors=$5, updated_at=$6 WHERE id=$1`,
		lib.ID, lib.TrackCount, lib.Duration, lib.LastScannedAt, lib.LastScanErrors, lib.UpdatedAt)
	return err
}

func (r *LibraryRepo) Delete(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM libraries WHERE id = $1`, id)
	return err
}
