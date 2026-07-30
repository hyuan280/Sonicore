package domain

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"
)

func NewID() string {
	b := make([]byte, 16)
	rand.Read(b)
	now := time.Now().UnixMilli()
	return fmt.Sprintf("%013d%018x", now, b)[:26]
}

type Role string

const (
	RoleSuperAdmin Role = "super_admin"
	RoleAdmin      Role = "admin"
	RoleUser       Role = "user"
)

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Library struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Path                string     `json:"path"`
	OwnerID             string     `json:"owner_id"`
	MetadataStorageMode string     `json:"metadata_storage_mode"`
	ScanInterval        string     `json:"scan_interval"`
	LastScannedAt       *time.Time `json:"last_scanned_at"`
	LastScanErrors      int        `json:"last_scan_errors"`
	TrackCount          int        `json:"track_count"`
	Duration            float64    `json:"duration"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`

	Owner   *User    `json:"owner,omitempty"`
	Members []*User  `json:"members,omitempty"`
}

type LibraryMember struct {
	LibraryID string    `json:"library_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	JoinedAt  time.Time `json:"joined_at"`
}

type Artist struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	SortName     string    `json:"sort_name"`
	MBID         string    `json:"mbid"`
	Country      string    `json:"country"`
	Biography    string    `json:"biography"`
	CoverImageID *string   `json:"cover_image_id,omitempty"`
	TrackCount   int       `json:"track_count"`
	Roles        []string  `json:"roles,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Album struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	ArtistID     string    `json:"artist_id"`
	MBID         string    `json:"mbid"`
	Country      string    `json:"country"`
	Year         int       `json:"year"`
	Genre        string    `json:"genre"`
	CoverImageID *string   `json:"cover_image_id,omitempty"`
	SongCount    int       `json:"song_count"`
	Duration     float64   `json:"duration"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	Artist *Artist `json:"artist,omitempty"`
}

type TrackAlbum struct {
	TrackID     string `json:"-"`
	AlbumID     string `json:"id"`
	TrackNumber int    `json:"track"`
	DiscNumber  int    `json:"disc_number"`
	Album       *Album `json:"album,omitempty"`
}

type Track struct {
	ID           string         `json:"id"`
	LibraryID    string         `json:"library_id"`
	Title        string         `json:"title"`
	CoverImageID *string        `json:"cover_image_id,omitempty"`
	Duration     float64        `json:"duration"`
	BitRate      int            `json:"bit_rate"`
	SampleRate   int            `json:"sample_rate"`
	Channels     int            `json:"channels"`
	FilePath     string         `json:"file_path"`
	FileSize     int64          `json:"file_size"`
	FileFormat   string         `json:"file_format"`
	MBID         string         `json:"mbid"`
	AcoustID     string         `json:"acoust_id"`
	Hash         string         `json:"hash"`
	HasLyrics    bool           `json:"has_lyrics"`
	Lyrics       string         `json:"lyrics"`
	Rating       int            `json:"rating"`
	PlayCount    int            `json:"play_count"`
	LastPlayedAt *time.Time     `json:"last_played_at,omitempty"`
	Metadata     *TrackMetadata `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`

	Albums  []*TrackAlbum  `json:"albums,omitempty"`
	Artist  *Artist        `json:"-"`
	Artists []*TrackArtist `json:"artists,omitempty"`
}

type TrackArtist struct {
	TrackID   string  `json:"track_id"`
	ArtistID  string  `json:"artist_id"`
	Role      string  `json:"role"`
	SortOrder int     `json:"sort_order"`
	Artist    *Artist `json:"artist,omitempty"`
}

type TrackMetadata struct {
	Composer    string       `json:"composer,omitempty"`
	Conductor   string       `json:"conductor,omitempty"`
	ISRC        string       `json:"isrc,omitempty"`
	BPM         int          `json:"bpm,omitempty"`
	Key         string       `json:"key,omitempty"`
	Mood        string       `json:"mood,omitempty"`
	Grouping    string       `json:"grouping,omitempty"`
	Comment     string       `json:"comment,omitempty"`
	MusicBrainz *MBMetadata  `json:"mb,omitempty"`
}

type MBMetadata struct {
	ArtistID       string `json:"artist_id,omitempty"`
	AlbumID        string `json:"album_id,omitempty"`
	TrackID        string `json:"track_id,omitempty"`
	RecordingID    string `json:"recording_id,omitempty"`
	WorkID         string `json:"work_id,omitempty"`
	ReleaseGroupID string `json:"release_group_id,omitempty"`
}

type Image struct {
	ID        string         `json:"id"`
	LibraryID string         `json:"library_id"`
	OwnerType string         `json:"owner_type"`
	OwnerID   string         `json:"owner_id"`
	Source    string         `json:"source"`
	Path      string         `json:"path"`
	Format    string         `json:"format"`
	Width     int            `json:"width"`
	Height    int            `json:"height"`
	Size      int64          `json:"size"`
	Hash      string         `json:"hash"`
	Variants  ImageVariants  `json:"variants,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type ImageVariant struct {
	Path   string `json:"path"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int64  `json:"size"`
}

type ImageVariants []ImageVariant

func (v ImageVariants) Value() ([]byte, error) {
	return json.Marshal(v)
}

func (v *ImageVariants) Scan(src interface{}) error {
	if src == nil {
		return nil
	}
	return json.Unmarshal(src.([]byte), v)
}

type Playlist struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OwnerID   string    `json:"owner_id"`
	IsPublic  bool      `json:"is_public"`
	TrackIDs  []string  `json:"track_ids"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DownloadJob struct {
	ID         string    `json:"id"`
	URL        string    `json:"url"`
	Source     string    `json:"source"`
	LibraryID  string    `json:"library_id,omitempty"`
	Format     string    `json:"format"`
	Status     string    `json:"status"`
	Progress   float64   `json:"progress"`
	TargetPath string    `json:"target_path"`
	Metadata   string    `json:"metadata,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ScanJob struct {
	ID            string     `json:"id"`
	LibraryID     string     `json:"library_id"`
	Type          string     `json:"type"`
	Status        string     `json:"status"`
	TotalFiles    int        `json:"total_files"`
	Scanned       int        `json:"scanned"`
	NewTracks     int        `json:"new_tracks"`
	UpdatedTracks int        `json:"updated_tracks"`
	DeletedTracks int        `json:"deleted_tracks"`
	Errors        string     `json:"errors,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

type RefreshToken struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type Favorite struct {
	UserID    string     `json:"user_id"`
	ItemType  string     `json:"item_type"` // track | album | artist
	ItemID    string     `json:"item_id"`
	LibraryID *string    `json:"library_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type PlayHistory struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TrackID   string    `json:"track_id"`
	LibraryID string    `json:"library_id"`
	PlayedAt  time.Time `json:"played_at"`
}

type UserSetting struct {
	UserID string `json:"user_id"`
	Key    string `json:"key"`
	Value  string `json:"value"`
}

type Jukebox struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	DeviceID        string            `json:"device_id"`   // physical device id (e.g. "aec_playback", "hw:1,0")
	DeviceConfigID  string            `json:"device_config_id,omitempty"` // FK to audio_devices
	DeviceName      string            `json:"device_name"` // cached display name
	DeviceDriver    string            `json:"device_driver,omitempty"` // pulseaudio, alsa, mpd, ...
	Volume          float64           `json:"volume"`
	PlayMode        string            `json:"play_mode"`
	Queue           []string          `json:"queue"`
	QueueIdx        int               `json:"queue_idx"`
	ShuffleOrder    []int             `json:"shuffle_order"`
	ShuffleIdx      int               `json:"shuffle_idx"`
	PathMapping     map[string]string `json:"path_mapping"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

type AudioDeviceConfig struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	DeviceType string            `json:"device_type"` // local, mpd, airplay, ...
	DeviceID   string            `json:"device_id"`   // physical device identifier
	Driver     string            `json:"driver"`      // pulseaudio, alsa, mpd, airplay
	Config     map[string]string `json:"config"`      // type-specific: host, port, password, ...
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}
