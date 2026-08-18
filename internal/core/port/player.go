package port

type PlaybackState struct {
	TrackID  string  `json:"track_id"`
	Status   string  `json:"status"`
	Position float64 `json:"position"`
	Duration float64 `json:"duration"`
	Volume   float64 `json:"volume"`
}

type Player interface {
	Play(trackID string) error
	Pause() error
	Stop() error
	Next() error
	Previous() error
	Seek(position float64) error
	SetVolume(volume float64) error
	GetState() (*PlaybackState, error)
	GetQueue() []string
	AddToQueue(trackIDs []string) error
	RemoveFromQueue(index int) error
	ClearQueue() error
	SetLoopMode(mode string) error
}
