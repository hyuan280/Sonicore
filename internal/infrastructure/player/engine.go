package player

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	audioDriver   string
	driverChecked bool
	driverCheckMu sync.Mutex
)

func detectAudioDriver() string {
	driverCheckMu.Lock()
	defer driverCheckMu.Unlock()
	if driverChecked {
		return audioDriver
	}
	driverChecked = true

	if tryPactl() {
		audioDriver = "pulseaudio"
		log.Printf("[audio] detected driver: pulseaudio")
		return audioDriver
	}
	if _, err := os.Stat("/proc/asound/cards"); err == nil {
		audioDriver = "alsa"
		log.Printf("[audio] detected driver: alsa")
		return audioDriver
	}
	audioDriver = "default"
	log.Printf("[audio] no audio driver detected, using sdl default")
	return audioDriver
}

func tryPactl() bool {
	s := os.Getenv("PULSE_SERVER")
	if s == "" {
		return false
	}
	cmd := exec.Command("pactl", "info")
	cmd.Env = append(os.Environ(), "PULSE_SERVER="+s, "LC_ALL=C")
	return cmd.Run() == nil
}

type State string
type PlayMode string

const (
	StateStopped  State    = "stopped"
	StatePlaying  State    = "playing"
	ModeNormal    PlayMode = "normal"
	ModeRepeatAll PlayMode = "repeat_all"
	ModeRepeatOne PlayMode = "repeat_one"
	ModeShuffle   PlayMode = "shuffle"
)

type TrackInfo struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Artist    string  `json:"artist"`
	FilePath  string  `json:"file_path"`
	Duration  float64 `json:"duration"`
	LibraryID string  `json:"library_id,omitempty"`
}

type Status struct {
	State        State      `json:"state"`
	Track        *TrackInfo `json:"track,omitempty"`
	Duration     float64    `json:"duration"`
	Volume       float64    `json:"volume"`
	PlayMode     PlayMode   `json:"play_mode"`
	Position     float64    `json:"position"`
	Queue        []string   `json:"queue"`
	QueueIdx     int        `json:"queue_idx"`
	ShuffleOrder []int      `json:"shuffle_order"`
	ShuffleIdx   int        `json:"shuffle_idx"`
}

type TrackResolver func(id string) (*TrackInfo, error)

type Engine struct {
	id       string
	deviceID string
	driver   string
	resolve  TrackResolver

	mu           sync.Mutex
	cancel       context.CancelFunc
	pid          int
	current      *TrackInfo
	state        State
	volume       float64
	playMode     PlayMode
	queue        []string
	queueIdx     int
	shuffleOrder []int
	shuffleIdx   int
	pathMapping  map[string]string
	libPaths     map[string]string
	playEpoch    uint64
	trackStart   time.Time
	onChange     func(Status)
}

func NewEngine(id, deviceID, driver string, resolver TrackResolver) *Engine {
	return &Engine{
		id:       id,
		deviceID: deviceID,
		driver:   driver,
		resolve:  resolver,
		state:    StateStopped,
		volume:   0.8,
		playMode: ModeNormal,
	}
}

func (e *Engine) ID() string               { return e.id }
func (e *Engine) DeviceID() string         { return e.deviceID }
func (e *Engine) OnChange(fn func(Status)) { e.onChange = fn }

func (e *Engine) RestoreState(queue []string, queueIdx int, shuffleOrder []int, shuffleIdx int, playMode string, volume float64) {
	e.mu.Lock()
	e.queue = queue
	if e.queue == nil {
		e.queue = []string{}
	}
	e.queueIdx = queueIdx
	e.shuffleOrder = shuffleOrder
	e.shuffleIdx = shuffleIdx
	e.playMode = PlayMode(playMode)
	e.volume = volume
	e.state = StateStopped
	e.current = nil
	e.pid = 0
	e.mu.Unlock()
	e.emit()
}

func (e *Engine) SyncVolume() {
	e.mu.Lock()
	vol := e.volume
	dev := e.deviceID
	drv := e.driver
	e.mu.Unlock()
	if drv == "pulseaudio" {
		setPulseSinkVolume(dev, vol)
	}
}

func (e *Engine) SetPathMapping(remote map[string]string, libPaths map[string]string) {
	e.mu.Lock()
	e.pathMapping = remote
	if e.pathMapping == nil {
		e.pathMapping = make(map[string]string)
	}
	e.libPaths = libPaths
	if e.libPaths == nil {
		e.libPaths = make(map[string]string)
	}
	e.mu.Unlock()
}

func (e *Engine) applyPathMapping(path, libID string) string {
	e.mu.Lock()
	remotePath := e.pathMapping[libID]
	localPath := e.libPaths[libID]
	e.mu.Unlock()

	if remotePath == "" || localPath == "" {
		return path
	}
	if strings.HasPrefix(path, localPath) {
		return remotePath + path[len(localPath):]
	}
	return path
}

func (e *Engine) Play(id string, info *TrackInfo) error {
	mappedPath := e.applyPathMapping(info.FilePath, info.LibraryID)

	e.mu.Lock()
	e.killLocked()
	e.playEpoch++
	e.trackStart = time.Now()
	e.current = info
	e.state = StatePlaying
	for i, tid := range e.queue {
		if tid == id {
			e.queueIdx = i
			if e.playMode == ModeShuffle {
				for j, idx := range e.shuffleOrder {
					if idx == i {
						e.shuffleIdx = j
						break
					}
				}
			}
			break
		}
	}
	vol := "100"
	volPct := e.volume * 100
	ctx, cancel := context.WithCancel(context.Background())
	e.cancel = cancel
	dev := e.deviceID
	e.mu.Unlock()

	log.Printf("[engine] Play: id=%s title=%q path=%q mapped=%q dev=%q vol=%.0f%%",
		id, info.Title, info.FilePath, mappedPath, dev, volPct)

	cmd := exec.CommandContext(ctx, "ffplay",
		"-nodisp", "-autoexit", "-loglevel", "warning",
		"-volume", vol, mappedPath,
	)
	cmd.Stderr = os.Stderr
	if dev != "" && dev != "default" {
		switch detectAudioDriver() {
		case "pulseaudio":
			cmd.Env = append(os.Environ(), "SDL_AUDIODRIVER=pulseaudio", "PULSE_SINK="+dev)
			log.Printf("[engine] using pulseaudio sink: %s", dev)
		case "alsa":
			cmd.Env = append(os.Environ(), "SDL_AUDIODRIVER=alsa", "AUDIODEV="+dev)
			log.Printf("[engine] using alsa device: %s", dev)
		default:
			log.Printf("[engine] unknown driver, passing device=%s", dev)
		}
	} else {
		// ensure non-alsa driver when no device specified
		d := detectAudioDriver()
		if d == "pulseaudio" {
			cmd.Env = append(os.Environ(), "SDL_AUDIODRIVER=pulseaudio")
		}
		_ = d
	}
	e.emit()

	if err := cmd.Start(); err != nil {
		log.Printf("[engine] ffplay start error: %v", err)
		e.mu.Lock()
		e.state = StateStopped
		e.mu.Unlock()
		e.emit()
		return nil
	}
	myPid := cmd.Process.Pid
	e.mu.Lock()
	e.pid = myPid
	e.mu.Unlock()
	log.Printf("[engine] ffplay started: pid=%d", myPid)

	go func(myTrack string, myPid int) {
		err := cmd.Wait()
		log.Printf("[engine] ffplay exited: id=%s pid=%d err=%v", myTrack, myPid, err)
		time.Sleep(300 * time.Millisecond)

		e.mu.Lock()
		if e.state != StatePlaying || e.pid != myPid || (e.current != nil && e.current.ID != myTrack) {
			e.mu.Unlock()
			return
		}
		e.pid = 0
		e.mu.Unlock()

		e.playNext()
	}(id, myPid)

	return nil
}

func (e *Engine) Stop() {
	e.mu.Lock()
	e.killLocked()
	e.mu.Unlock()
	e.emit()
}

func (e *Engine) Next() {
	e.mu.Lock()
	e.killLocked()
	advance := false
	var nextID string
	if e.playMode == ModeShuffle && len(e.shuffleOrder) > 0 && e.shuffleIdx < len(e.shuffleOrder)-1 {
		e.shuffleIdx++
		idx := e.shuffleOrder[e.shuffleIdx]
		if idx >= 0 && idx < len(e.queue) {
			nextID = e.queue[idx]
			advance = true
		}
	} else if len(e.queue) > 0 {
		if e.queueIdx < len(e.queue)-1 {
			e.queueIdx++
			nextID = e.queue[e.queueIdx]
			advance = true
		}
	}
	e.mu.Unlock()

	if !advance || nextID == "" {
		e.emit()
		return
	}

	if e.resolve != nil {
		info, err := e.resolve(nextID)
		if err == nil {
			e.Play(nextID, info)
			return
		}
	}
	e.mu.Lock()
	e.state = StateStopped
	e.current = nil
	e.mu.Unlock()
	e.emit()
}

func (e *Engine) Prev() {
	e.mu.Lock()
	e.killLocked()
	var nextID string
	if e.playMode == ModeShuffle && len(e.shuffleOrder) > 0 && e.shuffleIdx > 0 {
		e.shuffleIdx--
		idx := e.shuffleOrder[e.shuffleIdx]
		if idx >= 0 && idx < len(e.queue) {
			nextID = e.queue[idx]
		}
	} else if e.queueIdx > 0 {
		e.queueIdx--
		nextID = e.queue[e.queueIdx]
	}
	e.mu.Unlock()

	if nextID == "" {
		e.emit()
		return
	}

	if e.resolve != nil {
		info, err := e.resolve(nextID)
		if err == nil {
			e.Play(nextID, info)
			return
		}
	}
	e.mu.Lock()
	e.state = StateStopped
	e.current = nil
	e.mu.Unlock()
	e.emit()
}

func (e *Engine) playPrev() {
	e.mu.Lock()
	mode := e.playMode
	queueLen := len(e.queue)

	var prevID string
	switch mode {
	case ModeShuffle:
		if e.shuffleIdx > 0 {
			e.shuffleIdx--
			idx := e.shuffleOrder[e.shuffleIdx]
			if idx >= 0 && idx < queueLen {
				prevID = e.queue[idx]
			}
		}
	default:
		if e.queueIdx > 0 {
			e.queueIdx--
			prevID = e.queue[e.queueIdx]
		}
	}
	e.mu.Unlock()

	log.Printf("[engine] playPrev: mode=%s queueLen=%d prevID=%q", mode, queueLen, prevID)
	if prevID == "" {
		e.mu.Lock()
		e.state = StateStopped
		e.current = nil
		e.mu.Unlock()
		e.emit()
		return
	}

	if e.resolve != nil {
		info, err := e.resolve(prevID)
		if err == nil {
			e.Play(prevID, info)
			return
		}
	}
	e.mu.Lock()
	e.state = StateStopped
	e.current = nil
	e.mu.Unlock()
	e.emit()
}

func (e *Engine) playNext() {
	e.mu.Lock()
	mode := e.playMode
	queueLen := len(e.queue)

	var nextID string
	var skipLocked bool

	switch mode {
	case ModeRepeatOne:
		if e.current != nil {
			nextID = e.current.ID
			skipLocked = true
		}
	case ModeShuffle:
		if len(e.shuffleOrder) > 0 && e.shuffleIdx < len(e.shuffleOrder)-1 {
			e.shuffleIdx++
			idx := e.shuffleOrder[e.shuffleIdx]
			if idx >= 0 && idx < queueLen {
				nextID = e.queue[idx]
			}
		}
	case ModeRepeatAll:
		if queueLen > 0 {
			e.queueIdx = (e.queueIdx + 1) % queueLen
			nextID = e.queue[e.queueIdx]
		}
	default:
		if e.queueIdx < queueLen-1 {
			e.queueIdx++
			nextID = e.queue[e.queueIdx]
		}
	}
	e.mu.Unlock()

	log.Printf("[engine] playNext: mode=%s queueLen=%d nextID=%q", mode, queueLen, nextID)
	if nextID == "" {
		e.mu.Lock()
		e.state = StateStopped
		e.current = nil
		e.mu.Unlock()
		e.emit()
		return
	}

	if skipLocked {
		info := e.current
		e.Play(nextID, &TrackInfo{
			ID:       info.ID,
			Title:    info.Title,
			Artist:   info.Artist,
			FilePath: info.FilePath,
			Duration: info.Duration,
		})
		return
	}

	if e.resolve != nil {
		info, err := e.resolve(nextID)
		if err == nil {
			e.Play(nextID, info)
			return
		}
	}

	e.mu.Lock()
	e.state = StateStopped
	e.current = nil
	e.mu.Unlock()
	e.emit()
}

func (e *Engine) killLocked() {
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	e.pid = 0
	e.state = StateStopped
}

func (e *Engine) SetVolume(vol float64) {
	e.mu.Lock()
	e.volume = vol
	dev := e.deviceID
	drv := e.driver
	e.mu.Unlock()
	e.emit()

	if drv == "pulseaudio" {
		setPulseSinkVolume(dev, vol)
	}
}

func setPulseSinkVolume(sink string, vol float64) {
	pct := fmt.Sprintf("%.0f%%", vol*100)
	cmd := exec.Command("pactl", "set-sink-volume", sink, pct)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	if s := pulseSocket(); s != "" {
		cmd.Env = append(cmd.Env, "PULSE_SERVER="+s)
	}
	log.Printf("[engine] set-sink-volume: %s %s", sink, pct)
	if err := cmd.Run(); err != nil {
		log.Printf("[engine] set-sink-volume: %s %s error: %v", sink, pct, err)
	}
}

func pulseSocket() string {
	return os.Getenv("PULSE_SERVER")
}

func (e *Engine) SetPlayMode(mode PlayMode) {
	e.mu.Lock()
	e.playMode = mode
	if mode == ModeShuffle && len(e.queue) > 0 {
		e.shuffleOrder = rand.Perm(len(e.queue))
		e.shuffleIdx = 0
		if e.queueIdx >= 0 && e.queueIdx < len(e.queue) {
			// find current track in shuffle order and set shuffleIdx to match
			for i, idx := range e.shuffleOrder {
				if idx == e.queueIdx {
					e.shuffleIdx = i
					break
				}
			}
		}
	}
	e.mu.Unlock()
	e.emit()
}

func (e *Engine) AddToQueue(trackIDs []string) {
	e.mu.Lock()
	e.queue = append(e.queue, trackIDs...)
	if e.state == StateStopped && len(e.queue) > 0 {
		e.queueIdx = len(e.queue) - len(trackIDs)
		if e.playMode == ModeShuffle {
			e.shuffleOrder = rand.Perm(len(e.queue))
			e.shuffleIdx = e.queueIdx - 1
			if e.shuffleIdx < -1 {
				e.shuffleIdx = -1
			}
		}
	}
	e.mu.Unlock()
	e.emit()
}

func (e *Engine) RemoveFromQueue(index int) {
	e.mu.Lock()
	if index >= 0 && index < len(e.queue) {
		e.queue = append(e.queue[:index], e.queue[index+1:]...)
		if e.playMode == ModeShuffle {
			e.shuffleOrder = rand.Perm(len(e.queue))
			e.shuffleIdx = -1
		} else {
			if index < e.queueIdx && e.queueIdx > 0 {
				e.queueIdx--
			}
		}
	}
	e.mu.Unlock()
	e.emit()
}

func (e *Engine) ClearQueue() {
	e.mu.Lock()
	e.killLocked()
	e.queue = nil
	e.queueIdx = 0
	e.shuffleOrder = nil
	e.shuffleIdx = -1
	e.mu.Unlock()
	e.emit()
}

func (e *Engine) Shuffle() {
	e.mu.Lock()
	if len(e.queue) > 0 {
		e.shuffleOrder = rand.Perm(len(e.queue))
		e.playMode = ModeShuffle
		e.shuffleIdx = -1
	}
	e.mu.Unlock()
	e.emit()
}

func (e *Engine) SetQueue(trackIDs []string) {
	e.mu.Lock()
	e.queue = trackIDs
	e.queueIdx = 0
	e.shuffleOrder = nil
	e.shuffleIdx = -1
	if e.playMode == ModeShuffle && len(e.queue) > 0 {
		e.shuffleOrder = rand.Perm(len(e.queue))
	}
	e.mu.Unlock()
	e.emit()
}

func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	dur := 0.0
	pos := 0.0
	if e.current != nil {
		dur = e.current.Duration
		if e.state == StatePlaying {
			pos = time.Since(e.trackStart).Seconds()
			if pos > dur {
				pos = dur
			}
		}
	}
	return Status{
		State:        e.state,
		Track:        e.current,
		Duration:     dur,
		Position:     pos,
		Volume:       e.volume,
		PlayMode:     e.playMode,
		Queue:        e.queue,
		QueueIdx:     e.queueIdx,
		ShuffleOrder: e.shuffleOrder,
		ShuffleIdx:   e.shuffleIdx,
	}
}

func (e *Engine) emit() {
	if e.onChange != nil {
		e.onChange(e.Status())
	}
}

type EngineManager struct {
	mu      sync.RWMutex
	engines map[string]*Engine
}

func NewEngineManager() *EngineManager {
	return &EngineManager{
		engines: make(map[string]*Engine),
	}
}

func (m *EngineManager) GetOrCreate(id, deviceID, driver string, resolver TrackResolver) (*Engine, bool) {
	m.mu.RLock()
	e, ok := m.engines[id]
	m.mu.RUnlock()
	if ok {
		return e, false
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok = m.engines[id]; ok {
		return e, false
	}
	e = NewEngine(id, deviceID, driver, resolver)
	m.engines[id] = e
	return e, true
}

func (m *EngineManager) Get(id string) *Engine {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.engines[id]
}

func (m *EngineManager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.engines[id]; ok {
		e.Stop()
		delete(m.engines, id)
	}
}

func (m *EngineManager) ForEach(fn func(id string, eng *Engine)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, e := range m.engines {
		fn(id, e)
	}
}

func (m *EngineManager) StopAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.engines {
		e.Stop()
	}
}
