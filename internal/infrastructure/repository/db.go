package repository

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
	"github.com/sonicore/server/internal/config"
)

func NewDB(cfg config.DatabaseConfig) (*sql.DB, error) {
	db, err := sql.Open("postgres", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("[db] connected to PostgreSQL")
	return db, nil
}

func RunMigrations(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id          VARCHAR(26) PRIMARY KEY,
		username    VARCHAR(64) NOT NULL UNIQUE,
		email       VARCHAR(255) NOT NULL UNIQUE,
		password_hash VARCHAR(255) NOT NULL,
		role        VARCHAR(20) NOT NULL DEFAULT 'user',
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE TABLE IF NOT EXISTS libraries (
		id         VARCHAR(26) PRIMARY KEY,
		name       VARCHAR(128) NOT NULL,
		path       TEXT NOT NULL,
		owner_id   VARCHAR(26) NOT NULL REFERENCES users(id),
		metadata_storage_mode VARCHAR(20) NOT NULL DEFAULT 'database',
		scan_interval VARCHAR(20) NOT NULL DEFAULT '',
		last_scanned_at TIMESTAMPTZ,
		last_scan_errors INTEGER NOT NULL DEFAULT 0,
		track_count    INTEGER NOT NULL DEFAULT 0,
		duration       DOUBLE PRECISION NOT NULL DEFAULT 0,
		created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_libraries_owner ON libraries(owner_id);

	CREATE TABLE IF NOT EXISTS library_members (
		library_id VARCHAR(26) NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
		user_id    VARCHAR(26) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		role       VARCHAR(20) NOT NULL DEFAULT 'viewer',
		joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (library_id, user_id)
	);

	CREATE TABLE IF NOT EXISTS images (
		id         VARCHAR(26) PRIMARY KEY,
		-- Only track covers belong to a library; album/artist covers are
		-- shared and reference no library.
		library_id VARCHAR(26) REFERENCES libraries(id) ON DELETE CASCADE,
		owner_type VARCHAR(20) NOT NULL,
		owner_id   VARCHAR(26) NOT NULL,
		source     VARCHAR(20) NOT NULL,
		path       TEXT NOT NULL,
		format     VARCHAR(10) NOT NULL DEFAULT '',
		width      INTEGER NOT NULL DEFAULT 0,
		height     INTEGER NOT NULL DEFAULT 0,
		size       BIGINT NOT NULL DEFAULT 0,
		hash       VARCHAR(64) NOT NULL DEFAULT '',
		variants   JSONB,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		CONSTRAINT chk_images_track_library CHECK (
			owner_type IN ('track', 'album', 'artist') AND (
				(owner_type = 'track' AND library_id IS NOT NULL) OR
				(owner_type <> 'track' AND library_id IS NULL)
			)
		)
	);
	CREATE INDEX IF NOT EXISTS idx_images_owner ON images(owner_type, owner_id);
	CREATE INDEX IF NOT EXISTS idx_images_owner_path ON images(owner_type, path);
	CREATE INDEX IF NOT EXISTS idx_images_hash ON images(hash);
	-- SharedPaths (orphan sweep) filters on path alone, which cannot use
	-- the (owner_type, path) composite index's leftmost prefix.
	CREATE INDEX IF NOT EXISTS idx_images_path ON images(path);

	CREATE TABLE IF NOT EXISTS artists (
		id         VARCHAR(26) PRIMARY KEY,
		name       VARCHAR(255) NOT NULL,
		sort_name  VARCHAR(255) NOT NULL DEFAULT '',
		external_id VARCHAR(36) NOT NULL DEFAULT '',
		metadata_source VARCHAR(20) NOT NULL DEFAULT 'musicbrainz',
		external_ids    JSONB NOT NULL DEFAULT '{}',
		name_normalized VARCHAR(255) NOT NULL DEFAULT '',
		country    VARCHAR(4) NOT NULL DEFAULT '',
		biography  TEXT NOT NULL DEFAULT '',
		cover_image_id VARCHAR(26) REFERENCES images(id) ON DELETE SET NULL,
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_artists_name ON artists(name);
	CREATE INDEX IF NOT EXISTS idx_artists_external_id ON artists(external_id);
	CREATE INDEX IF NOT EXISTS idx_artists_source_id ON artists(metadata_source, external_id);
	CREATE INDEX IF NOT EXISTS idx_artists_external ON artists USING GIN (external_ids jsonb_path_ops);
	CREATE INDEX IF NOT EXISTS idx_artists_norm ON artists(name_normalized);

	CREATE TABLE IF NOT EXISTS albums (
		id           VARCHAR(26) PRIMARY KEY,
		title        VARCHAR(255) NOT NULL,
		artist_id    VARCHAR(26) NOT NULL REFERENCES artists(id),
		external_id  VARCHAR(36) NOT NULL DEFAULT '',
		metadata_source VARCHAR(20) NOT NULL DEFAULT 'musicbrainz',
		external_ids    JSONB NOT NULL DEFAULT '{}',
		title_normalized VARCHAR(255) NOT NULL DEFAULT '',
		country      VARCHAR(4) NOT NULL DEFAULT '',
		year         INTEGER NOT NULL DEFAULT 0,
		genre        VARCHAR(128) NOT NULL DEFAULT '',
		cover_image_id VARCHAR(26) REFERENCES images(id) ON DELETE SET NULL,
		song_count   INTEGER NOT NULL DEFAULT 0,
		duration     DOUBLE PRECISION NOT NULL DEFAULT 0,
		created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_albums_artist ON albums(artist_id);
	CREATE INDEX IF NOT EXISTS idx_albums_title ON albums(title);
	CREATE INDEX IF NOT EXISTS idx_albums_external_id ON albums(external_id);
	CREATE INDEX IF NOT EXISTS idx_albums_source_id ON albums(metadata_source, external_id);
	CREATE INDEX IF NOT EXISTS idx_albums_external ON albums USING GIN (external_ids jsonb_path_ops);
	CREATE INDEX IF NOT EXISTS idx_albums_norm ON albums(title_normalized, artist_id);

	CREATE TABLE IF NOT EXISTS tracks (
		id            VARCHAR(26) PRIMARY KEY,
		library_id    VARCHAR(26) NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
		title         VARCHAR(255) NOT NULL,
		cover_image_id VARCHAR(26) REFERENCES images(id) ON DELETE SET NULL,
		duration      DOUBLE PRECISION NOT NULL DEFAULT 0,
		bit_rate      INTEGER NOT NULL DEFAULT 0,
		sample_rate   INTEGER NOT NULL DEFAULT 0,
		channels      INTEGER NOT NULL DEFAULT 2,
		file_path     TEXT NOT NULL,
		file_size     BIGINT NOT NULL DEFAULT 0,
		file_format   VARCHAR(10) NOT NULL DEFAULT '',
		audio_codec   VARCHAR(32) NOT NULL DEFAULT '',
		external_id   VARCHAR(36) NOT NULL DEFAULT '',
		metadata_source VARCHAR(20) NOT NULL DEFAULT 'musicbrainz',
		external_ids  JSONB NOT NULL DEFAULT '{}',
		acoust_id     VARCHAR(36) NOT NULL DEFAULT '',
		hash          VARCHAR(64) NOT NULL DEFAULT '',
		lyrics_mask   SMALLINT NOT NULL DEFAULT 0,
		lyrics_offset DOUBLE PRECISION NOT NULL DEFAULT 0,
		heat        INTEGER NOT NULL DEFAULT 0,
		play_count    INTEGER NOT NULL DEFAULT 0,
		last_played_at TIMESTAMPTZ,
		metadata      JSONB,
		version       SMALLINT NOT NULL DEFAULT 0,
		version_label TEXT NOT NULL DEFAULT '',
		created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_tracks_library ON tracks(library_id);
	CREATE INDEX IF NOT EXISTS idx_tracks_hash ON tracks(hash);
	CREATE INDEX IF NOT EXISTS idx_tracks_title ON tracks(title);
	CREATE INDEX IF NOT EXISTS idx_tracks_filepath ON tracks(file_path);
	CREATE INDEX IF NOT EXISTS idx_tracks_external_id ON tracks(external_id);
	CREATE INDEX IF NOT EXISTS idx_tracks_external_ids ON tracks USING GIN (external_ids jsonb_path_ops);

	CREATE TABLE IF NOT EXISTS track_artists (
		track_id   VARCHAR(26) NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
		artist_id  VARCHAR(26) NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
		role       VARCHAR(30) NOT NULL DEFAULT 'performer',
		sort_order SMALLINT NOT NULL DEFAULT 0,
		PRIMARY KEY (track_id, artist_id, role)
	);
	CREATE INDEX IF NOT EXISTS idx_track_artists_artist ON track_artists(artist_id);

	CREATE TABLE IF NOT EXISTS track_albums (
		track_id      VARCHAR(26) NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
		album_id      VARCHAR(26) NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
		track_number  INTEGER NOT NULL DEFAULT 0,
		disc_number   INTEGER NOT NULL DEFAULT 1,
		PRIMARY KEY (track_id, album_id)
	);
	CREATE INDEX IF NOT EXISTS idx_track_albums_album ON track_albums(album_id);

	CREATE TABLE IF NOT EXISTS track_version_groups (
		metadata_source VARCHAR(20) NOT NULL DEFAULT 'musicbrainz',
		external_id VARCHAR(36) NOT NULL,
		library_id VARCHAR(26) NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
		track_id   VARCHAR(26) NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
		PRIMARY KEY (metadata_source, external_id, track_id)
	);
	CREATE INDEX IF NOT EXISTS idx_tvg_external_id ON track_version_groups(metadata_source, external_id);
	CREATE INDEX IF NOT EXISTS idx_tvg_library ON track_version_groups(library_id);

	CREATE TABLE IF NOT EXISTS playlists (
		id         VARCHAR(26) PRIMARY KEY,
		name       VARCHAR(128) NOT NULL,
		owner_id   VARCHAR(26) NOT NULL REFERENCES users(id),
		is_public  BOOLEAN NOT NULL DEFAULT FALSE,
		track_ids  JSONB NOT NULL DEFAULT '[]',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_playlists_owner ON playlists(owner_id);

	CREATE TABLE IF NOT EXISTS scan_jobs (
		id             VARCHAR(26) PRIMARY KEY,
		library_id     VARCHAR(26) NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
		type           VARCHAR(20) NOT NULL,
		status         VARCHAR(20) NOT NULL DEFAULT 'pending',
		total_files    INTEGER NOT NULL DEFAULT 0,
		scanned        INTEGER NOT NULL DEFAULT 0,
		new_tracks     INTEGER NOT NULL DEFAULT 0,
		updated_tracks INTEGER NOT NULL DEFAULT 0,
		deleted_tracks INTEGER NOT NULL DEFAULT 0,
		errors         TEXT NOT NULL DEFAULT '',
		created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		completed_at   TIMESTAMPTZ
	);
	CREATE INDEX IF NOT EXISTS idx_scan_jobs_library ON scan_jobs(library_id);

	CREATE TABLE IF NOT EXISTS download_jobs (
		id          VARCHAR(26) PRIMARY KEY,
		url         TEXT NOT NULL,
		source      VARCHAR(64) NOT NULL DEFAULT '',
		library_id  VARCHAR(26) REFERENCES libraries(id) ON DELETE CASCADE,
		format      VARCHAR(20) NOT NULL DEFAULT '',
		status      VARCHAR(20) NOT NULL DEFAULT 'queued',
		progress    DOUBLE PRECISION NOT NULL DEFAULT 0,
		target_path TEXT NOT NULL DEFAULT '',
		metadata    JSONB,
		error       TEXT NOT NULL DEFAULT '',
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_download_jobs_library ON download_jobs(library_id);
	CREATE INDEX IF NOT EXISTS idx_download_jobs_status ON download_jobs(status);

	CREATE TABLE IF NOT EXISTS favorites (
		user_id    VARCHAR(26) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		item_type  VARCHAR(10) NOT NULL,
		item_id    VARCHAR(26) NOT NULL,
		library_id VARCHAR(26) REFERENCES libraries(id) ON DELETE CASCADE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (user_id, item_type, item_id)
	);
	CREATE INDEX IF NOT EXISTS idx_favorites_library ON favorites(library_id);

	CREATE TABLE IF NOT EXISTS play_history (
		id         VARCHAR(26) PRIMARY KEY,
		user_id    VARCHAR(26) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		track_id   VARCHAR(26) NOT NULL,
		library_id VARCHAR(26) NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
		played_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_play_history_user ON play_history(user_id, played_at DESC);
	CREATE INDEX IF NOT EXISTS idx_play_history_library ON play_history(library_id);

	CREATE TABLE IF NOT EXISTS user_settings (
		user_id VARCHAR(26) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		key     VARCHAR(64) NOT NULL,
		value   TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, key)
	);

	CREATE TABLE IF NOT EXISTS audio_devices (
		id          VARCHAR(26) PRIMARY KEY,
		name        VARCHAR(128) NOT NULL,
		device_type VARCHAR(20) NOT NULL DEFAULT 'local',
		device_id   VARCHAR(255) NOT NULL,
		driver      VARCHAR(20) NOT NULL DEFAULT 'pulseaudio',
		config      JSONB NOT NULL DEFAULT '{}',
		created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE TABLE IF NOT EXISTS jukeboxes (
		id              VARCHAR(26) PRIMARY KEY,
		name            VARCHAR(128) NOT NULL,
		device_id       VARCHAR(255) NOT NULL DEFAULT 'default',
		device_name     VARCHAR(255) NOT NULL DEFAULT '',
		device_config_id VARCHAR(26) REFERENCES audio_devices(id) ON DELETE SET NULL,
		device_driver   VARCHAR(255) NOT NULL DEFAULT '',
		volume          DOUBLE PRECISION NOT NULL DEFAULT 0.8,
		play_mode       VARCHAR(20) NOT NULL DEFAULT 'normal',
		queue           JSONB NOT NULL DEFAULT '[]',
		queue_idx       INTEGER NOT NULL DEFAULT 0,
		shuffle_order   JSONB NOT NULL DEFAULT '[]',
		shuffle_idx     INTEGER NOT NULL DEFAULT 0,
		path_mapping    JSONB NOT NULL DEFAULT '{}',
		created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE TABLE IF NOT EXISTS server_settings (
		key   VARCHAR(64) PRIMARY KEY,
		value TEXT NOT NULL DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS user_metadata (
		user_id          VARCHAR(26) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		file_hash        VARCHAR(64) NOT NULL,
		metadata_source  VARCHAR(20) NOT NULL DEFAULT 'musicbrainz',
		external_id      VARCHAR(36) NOT NULL DEFAULT '',
		title            VARCHAR(255) NOT NULL DEFAULT '',
		artist           VARCHAR(255) NOT NULL DEFAULT '',
		album            VARCHAR(255) NOT NULL DEFAULT '',
		album_artist     VARCHAR(255) NOT NULL DEFAULT '',
		track_number     INTEGER NOT NULL DEFAULT 0,
		disc_number      INTEGER NOT NULL DEFAULT 0,
		year             INTEGER NOT NULL DEFAULT 0,
		genre            VARCHAR(128) NOT NULL DEFAULT '',
		updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		PRIMARY KEY (user_id, file_hash)
	);

	INSERT INTO server_settings (key, value) VALUES ('allow_registration', 'true')
		ON CONFLICT (key) DO NOTHING;
	`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("schema migration failed: %w", err)
	}

	log.Println("[migrate] database migration completed")
	return nil
}
