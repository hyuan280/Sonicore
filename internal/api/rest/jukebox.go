package rest

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/sonicore/server/internal/api/ws"
	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/infrastructure/player"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type JukeboxHandler struct {
	db              *sql.DB
	manager         *player.EngineManager
	jukeboxRepo     *repository.JukeboxRepo
	audioDeviceRepo *repository.AudioDeviceRepo
	trackRepo       *repository.TrackRepo
	wsHub           *ws.Hub
}

func NewJukeboxHandler(db *sql.DB, manager *player.EngineManager, wsHub *ws.Hub) *JukeboxHandler {
	return &JukeboxHandler{
		db:              db,
		manager:         manager,
		jukeboxRepo:     repository.NewJukeboxRepo(db),
		audioDeviceRepo: repository.NewAudioDeviceRepo(db),
		trackRepo:       repository.NewTrackRepo(db),
		wsHub:           wsHub,
	}
}

func (h *JukeboxHandler) getLibPaths(ctx context.Context) map[string]string {
	out := make(map[string]string)
	rows, err := h.db.QueryContext(ctx, "SELECT id, path FROM libraries")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err == nil {
			out[id] = path
		}
	}
	return out
}

func (h *JukeboxHandler) getOrCreateEngine(id, deviceID, driver string) (*player.Engine, bool) {
	return h.manager.GetOrCreate(id, deviceID, driver, func(trackID string) (*player.TrackInfo, error) {
		track, err := h.trackRepo.FindByID(context.Background(), trackID)
		if err != nil {
			log.Printf("[jukebox] resolve failed: id=%q err=%v", trackID, err)
			return nil, err
		}
		log.Printf("[jukebox] resolved: id=%q path=%q lib=%q", trackID, track.FilePath, track.LibraryID)
		info := &player.TrackInfo{
			ID:        track.ID,
			Title:     track.Title,
			FilePath:  track.FilePath,
			Duration:  track.Duration,
			LibraryID: track.LibraryID,
		}
		if track.Artist != nil {
			info.Artist = track.Artist.Name
		}
		return info, nil
	})
}

func (h *JukeboxHandler) wireEngine(eng *player.Engine) {
	eng.OnChange(func(s player.Status) {
		h.wsHub.Broadcast("jukebox:"+eng.ID(), map[string]interface{}{
			"event":  "state_change",
			"status": s,
		})

		go func() {
			if err := h.jukeboxRepo.SaveState(context.Background(), eng.ID(),
				s.Queue, s.QueueIdx, s.ShuffleOrder, s.ShuffleIdx,
				string(s.PlayMode), s.Volume); err != nil {
				log.Printf("[jukebox] save state error: %v", err)
			}
		}()
	})
}

func (h *JukeboxHandler) ensureEngine(ctx context.Context, id string) (*player.Engine, error) {
	j, err := h.jukeboxRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	eng, newEngine := h.getOrCreateEngine(id, j.DeviceID, j.DeviceDriver)
	h.wireEngine(eng)
	eng.SetPathMapping(j.PathMapping, h.getLibPaths(ctx))
	if newEngine {
		eng.RestoreState(j.Queue, j.QueueIdx, j.ShuffleOrder, j.ShuffleIdx, j.PlayMode, j.Volume)
		go eng.SyncVolume()
	}
	return eng, nil
}

// ---- CRUD ----

func (h *JukeboxHandler) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.jukeboxRepo.List(r.Context())
	if err != nil {
		log.Printf("[jukebox] List failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type summary struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		DeviceName string `json:"device_name"`
		IsPlaying  bool   `json:"is_playing"`
	}
	result := make([]summary, 0, len(list))
	for _, j := range list {
		isPlaying := false
		if eng := h.manager.Get(j.ID); eng != nil {
			status := eng.Status()
			isPlaying = status.State == player.StatePlaying
		} else {
			// First access after restart — create engine to get actual state
			if eng, err := h.ensureEngine(r.Context(), j.ID); err == nil {
				status := eng.Status()
				isPlaying = status.State == player.StatePlaying
			}
		}
		result = append(result, summary{
			ID:         j.ID,
			Name:       j.Name,
			DeviceName: j.DeviceName,
			IsPlaying:  isPlaying,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"jukeboxes": result})
}

func (h *JukeboxHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string `json:"name"`
		DeviceID       string `json:"device_id"`
		DeviceConfigID string `json:"device_config_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	j := &domain.Jukebox{
		ID:         domain.NewID(),
		Name:       req.Name,
		Volume:     0.8,
		PlayMode:   "normal",
	}

	if req.DeviceConfigID != "" {
		devCfg, err := h.audioDeviceRepo.GetByID(r.Context(), req.DeviceConfigID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "device config not found"})
			return
		}
		// check if already bound
		if bound, _ := h.audioDeviceRepo.GetBoundJukebox(r.Context(), req.DeviceConfigID); bound != "" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "device already bound to another jukebox"})
			return
		}
		j.DeviceConfigID = req.DeviceConfigID
		j.DeviceID = devCfg.DeviceID
		j.DeviceName = devCfg.Name
		j.DeviceDriver = devCfg.Driver
	} else if req.DeviceID != "" {
		j.DeviceID = req.DeviceID
		j.DeviceName = req.DeviceID
	} else {
		j.DeviceID = "default"
		j.DeviceName = "System Default"
	}

	if err := h.jukeboxRepo.Create(r.Context(), j); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if j.DeviceID != "" {
		eng, _ := h.getOrCreateEngine(j.ID, j.DeviceID, j.DeviceDriver)
		h.wireEngine(eng)
		go eng.SyncVolume()
	}

	writeJSON(w, http.StatusCreated, j)
}

func (h *JukeboxHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	j, err := h.jukeboxRepo.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":            j.ID,
		"name":          j.Name,
		"device_name":   j.DeviceName,
		"device_id":     j.DeviceID,
		"volume":        j.Volume,
		"play_mode":     j.PlayMode,
		"path_mapping":  j.PathMapping,
	})
}

func (h *JukeboxHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	j, err := h.jukeboxRepo.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	var req struct {
		Name     *string `json:"name"`
		DeviceID *string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Name != nil {
		j.Name = *req.Name
	}
	if req.DeviceID != nil {
		j.DeviceID = *req.DeviceID
		for _, d := range player.DetectAudioDevices() {
			if d.ID == *req.DeviceID {
				j.DeviceName = d.Name
				break
			}
		}
	}

	if err := h.jukeboxRepo.Update(r.Context(), j); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	h.manager.Remove(id)
	eng, _ := h.getOrCreateEngine(id, j.DeviceID, j.DeviceDriver)
	h.wireEngine(eng)
	go eng.SyncVolume()

	writeJSON(w, http.StatusOK, j)
}

func (h *JukeboxHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := h.jukeboxRepo.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.manager.Remove(id)
	writeJSON(w, http.StatusNoContent, nil)
}

// ---- Playback ----

func (h *JukeboxHandler) Status(w http.ResponseWriter, r *http.Request) {
	eng, err := h.ensureEngine(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "jukebox not found"})
		return
	}
	writeJSON(w, http.StatusOK, eng.Status())
}

func (h *JukeboxHandler) Play(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	trackID := mux.Vars(r)["trackId"]

	eng, err := h.ensureEngine(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "jukebox not found"})
		return
	}

	track, err := h.trackRepo.FindByID(r.Context(), trackID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "track not found"})
		return
	}

	info := &player.TrackInfo{
		ID:        track.ID,
		Title:     track.Title,
		FilePath:  track.FilePath,
		Duration:  track.Duration,
		LibraryID: track.LibraryID,
	}
	if track.Artist != nil {
		info.Artist = track.Artist.Name
	}

	if err := eng.Play(track.ID, info); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, eng.Status())
}

func (h *JukeboxHandler) Stop(w http.ResponseWriter, r *http.Request) {
	eng, err := h.ensureEngine(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "jukebox not found"})
		return
	}
	eng.Stop()
	writeJSON(w, http.StatusOK, eng.Status())
}

func (h *JukeboxHandler) Next(w http.ResponseWriter, r *http.Request) {
	eng, err := h.ensureEngine(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "jukebox not found"})
		return
	}
	eng.Next()
	writeJSON(w, http.StatusOK, eng.Status())
}

func (h *JukeboxHandler) Prev(w http.ResponseWriter, r *http.Request) {
	eng, err := h.ensureEngine(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "jukebox not found"})
		return
	}
	eng.Prev()
	writeJSON(w, http.StatusOK, eng.Status())
}

func (h *JukeboxHandler) Volume(w http.ResponseWriter, r *http.Request) {
	eng, err := h.ensureEngine(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "jukebox not found"})
		return
	}
	var req struct {
		Volume float64 `json:"volume"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	eng.SetVolume(req.Volume)
	writeJSON(w, http.StatusOK, eng.Status())
}

func (h *JukeboxHandler) PlayMode(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	eng, err := h.ensureEngine(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "jukebox not found"})
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	eng.SetPlayMode(player.PlayMode(req.Mode))

	if j, err := h.jukeboxRepo.GetByID(r.Context(), id); err == nil {
		j.PlayMode = req.Mode
		h.jukeboxRepo.Update(r.Context(), j)
	}

	writeJSON(w, http.StatusOK, eng.Status())
}

// ---- Queue ----

func (h *JukeboxHandler) Queue(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	switch r.Method {
	case http.MethodGet:
		eng, err := h.ensureEngine(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "jukebox not found"})
			return
		}
		writeJSON(w, http.StatusOK, eng.Status())

	case http.MethodPost:
		eng, err := h.ensureEngine(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "jukebox not found"})
			return
		}

		var req struct {
			TrackIDs []string `json:"track_ids"`
			PlayNow  bool     `json:"play_now"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		eng.AddToQueue(req.TrackIDs)
		if req.PlayNow && len(req.TrackIDs) > 0 {
			writeJSON(w, http.StatusOK, map[string]string{"status": "queued", "auto_play": "pending"})
		} else {
			writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
		}

	case http.MethodDelete:
		eng, err := h.ensureEngine(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "jukebox not found"})
			return
		}
		eng.ClearQueue()
		writeJSON(w, http.StatusOK, eng.Status())

	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (h *JukeboxHandler) RemoveFromQueue(w http.ResponseWriter, r *http.Request) {
	eng, err := h.ensureEngine(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "jukebox not found"})
		return
	}
	idxStr := mux.Vars(r)["index"]
	idx, _ := strconv.Atoi(idxStr)
	eng.RemoveFromQueue(idx)
	writeJSON(w, http.StatusOK, eng.Status())
}

func (h *JukeboxHandler) Shuffle(w http.ResponseWriter, r *http.Request) {
	eng, err := h.ensureEngine(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "jukebox not found"})
		return
	}
	eng.Shuffle()
	writeJSON(w, http.StatusOK, eng.Status())
}

func (h *JukeboxHandler) SetQueue(w http.ResponseWriter, r *http.Request) {
	eng, err := h.ensureEngine(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "jukebox not found"})
		return
	}

	var req struct {
		TrackIDs []string `json:"track_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	eng.SetQueue(req.TrackIDs)
	writeJSON(w, http.StatusOK, map[string]string{"status": "queue set"})
}

// ---- Audio Devices ----

func (h *JukeboxHandler) AudioDevices(w http.ResponseWriter, r *http.Request) {
	devices := player.DetectAudioDevices()
	writeJSON(w, http.StatusOK, map[string]interface{}{"devices": devices})
}

func (h *JukeboxHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		PathMapping map[string]string `json:"path_mapping"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	// clean empty destination paths
	for k, v := range req.PathMapping {
		if strings.TrimSpace(v) == "" {
			delete(req.PathMapping, k)
		}
	}

	if err := h.jukeboxRepo.UpdateSettings(r.Context(), id, req.PathMapping); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if eng := h.manager.Get(id); eng != nil {
		eng.SetPathMapping(req.PathMapping, h.getLibPaths(r.Context()))
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---- Audio Device Configs ----

func (h *JukeboxHandler) ListDeviceConfigs(w http.ResponseWriter, r *http.Request) {
	list, err := h.audioDeviceRepo.List(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if list == nil {
		list = []*domain.AudioDeviceConfig{}
	}

	type deviceWithBound struct {
		*domain.AudioDeviceConfig
		BoundJukebox string `json:"bound_jukebox,omitempty"`
	}
	result := make([]deviceWithBound, len(list))
	for i, d := range list {
		b := deviceWithBound{AudioDeviceConfig: d}
		if jbxID, err := h.audioDeviceRepo.GetBoundJukebox(r.Context(), d.ID); err == nil && jbxID != "" {
			if jbx, err := h.jukeboxRepo.GetByID(r.Context(), jbxID); err == nil {
				b.BoundJukebox = jbx.Name
			}
		}
		result[i] = b
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"devices": result})
}

func (h *JukeboxHandler) ListAvailableDeviceConfigs(w http.ResponseWriter, r *http.Request) {
	list, err := h.audioDeviceRepo.ListAvailable(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if list == nil {
		list = []*domain.AudioDeviceConfig{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"devices": list})
}

func (h *JukeboxHandler) CreateDeviceConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string            `json:"name"`
		DeviceType string            `json:"device_type"` // local, mpd, airplay
		DeviceID   string            `json:"device_id"`
		Driver     string            `json:"driver"`
		Config     map[string]string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Name == "" || req.DeviceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and device_id required"})
		return
	}
	if req.DeviceType == "" {
		req.DeviceType = "local"
	}
	if req.Driver == "" {
		req.Driver = "pulseaudio"
	}

	d := &domain.AudioDeviceConfig{
		ID:         domain.NewID(),
		Name:       req.Name,
		DeviceType: req.DeviceType,
		DeviceID:   req.DeviceID,
		Driver:     req.Driver,
		Config:     req.Config,
	}
	if d.Config == nil {
		d.Config = make(map[string]string)
	}

	if err := h.audioDeviceRepo.Create(r.Context(), d); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

func (h *JukeboxHandler) UpdateDeviceConfig(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	d, err := h.audioDeviceRepo.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	var req struct {
		Name       *string            `json:"name"`
		DeviceType *string            `json:"device_type"`
		DeviceID   *string            `json:"device_id"`
		Driver     *string            `json:"driver"`
		Config     map[string]string  `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Name != nil {
		d.Name = *req.Name
	}
	if req.DeviceType != nil {
		d.DeviceType = *req.DeviceType
	}
	if req.DeviceID != nil {
		d.DeviceID = *req.DeviceID
	}
	if req.Driver != nil {
		d.Driver = *req.Driver
	}
	if req.Config != nil {
		d.Config = req.Config
	}

	if err := h.audioDeviceRepo.Update(r.Context(), d); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (h *JukeboxHandler) DeleteDeviceConfig(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	boundJbx, err := h.audioDeviceRepo.GetBoundJukebox(r.Context(), id)
	if err == nil && boundJbx != "" {
		var jbxName string
		h.jukeboxRepo.GetByID(r.Context(), boundJbx)
		// try to get name
		if jbx, err := h.jukeboxRepo.GetByID(r.Context(), boundJbx); err == nil {
			jbxName = jbx.Name
		}
		writeJSON(w, http.StatusConflict, map[string]string{
			"error":     "device is bound to jukebox",
			"jukebox":   jbxName,
		})
		return
	}
	if err := h.audioDeviceRepo.Delete(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}
