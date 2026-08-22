package subsonic

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/service"
	"github.com/sonicore/server/internal/infrastructure/auth"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/player"
	"github.com/sonicore/server/internal/infrastructure/repository"
	"github.com/sonicore/server/internal/infrastructure/transcoder"
)

var errTokenNotSupported = errors.New("token auth not supported")

type Handler struct {
	db            *sql.DB
	jwt           *auth.JWTService
	userRepo      *repository.UserRepo
	trackRepo     *repository.TrackRepo
	albumRepo     *repository.AlbumRepo
	artistRepo    *repository.ArtistRepo
	libRepo       *repository.LibraryRepo
	playlistRepo  *repository.PlaylistRepo
	scanner       *service.ScannerService
	engineManager *player.EngineManager
	images        *repository.ImageRepo
}

func NewHandler(db *sql.DB, jwt *auth.JWTService, scanner *service.ScannerService, engineManager *player.EngineManager) *Handler {
	return &Handler{
		db:            db,
		jwt:           jwt,
		userRepo:      repository.NewUserRepo(db),
		images:        repository.NewImageRepo(db),
		trackRepo:     repository.NewTrackRepo(db),
		albumRepo:     repository.NewAlbumRepo(db),
		artistRepo:    repository.NewArtistRepo(db),
		libRepo:       repository.NewLibraryRepo(db),
		playlistRepo:  repository.NewPlaylistRepo(db),
		scanner:       scanner,
		engineManager: engineManager,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	action := strings.TrimRight(r.URL.Path, "/")
	action = strings.TrimPrefix(action, "/rest/")
	action = strings.Split(action, ".")[0]
	action = strings.Split(action, "/")[0]

	user, err := h.authenticate(r, q)
	if err != nil {
		logger.Error("[subsonic] auth failed: %v", err)
		code := 40
		message := "Wrong username or password"
		if errors.Is(err, errTokenNotSupported) {
			code = 41
			message = "Token authentication not supported for this user"
		}
		h.respond(w, r, "failed", map[string]interface{}{
			"error": map[string]interface{}{
				"code":    code,
				"message": message,
			},
		})
		return
	}

	ctx := r.Context()
	var body map[string]interface{}

	switch action {
	case "ping":
	case "getLicense":
		body = map[string]interface{}{
			"license": map[string]interface{}{
				"valid": true,
				"email": user.Email,
			},
		}
	case "getScanStatus":
		scanning := false
		var count int64 = 0
		libs, _ := h.libRepo.FindByUserID(ctx, user.ID)
		for _, lib := range libs {
			if p := h.scanner.GetProgress(lib.ID); p != nil {
				scanning = p.Status == "running"
				count = int64(p.Scanned)
				break
			}
		}
		body = map[string]interface{}{
			"scanStatus": map[string]interface{}{
				"scanning": scanning,
				"count":    count,
			},
		}
	case "startScan":
		libs, _ := h.libRepo.FindByUserID(ctx, user.ID)
		for _, lib := range libs {
			if err := h.scanner.StartScan(ctx, lib.ID, "missing"); err != nil {
				logger.Error("[subsonic] start scan error: %v", err)
			}
		}
		body = map[string]interface{}{
			"scanStatus": map[string]interface{}{
				"scanning": true,
				"count":    0,
			},
		}
	case "getIndexes", "getArtists":
		body = h.getArtists(ctx, user)
	case "getMusicFolders":
		body = h.getMusicFolders(ctx, user)
	case "getArtist":
		body = h.getArtist(ctx, q)
	case "getAlbum":
		body = h.getAlbum(ctx, q)
	case "getSong":
		body = h.getSong(ctx, q)
	case "getAlbumList", "getAlbumList2":
		body = h.getAlbumList(ctx, user, q)
	case "search2", "search3":
		body = h.search(ctx, user, q)
	case "stream":
		h.serveStream(w, r, q)
		return
	case "getCoverArt":
		h.serveCoverArt(w, r, q)
		return
	case "getPlaylists":
		body = h.getPlaylists(ctx, user)
	case "getPlaylist":
		body = h.getPlaylist(ctx, user, q)
	case "createPlaylist":
		h.createPlaylist(ctx, w, r, user, q)
		return
	case "deletePlaylist":
		h.deletePlaylist(ctx, w, r, user, q)
		return
	case "updatePlaylist":
		h.updatePlaylist(ctx, w, r, user, q)
		return
	case "getUser":
		body = h.getUser(ctx, user, q)
	case "getUsers":
		body = h.getUsers(ctx, user)
	case "createUser":
		body = h.createUser(ctx, user, q)
	case "updateUser":
		body = h.updateUser(ctx, user, q)
	case "deleteUser":
		body = h.deleteUser(ctx, user, q)
	case "changePassword":
		body = h.changePassword(ctx, user, q)
	case "getMusicDirectory":
		body = h.getMusicDirectory(ctx, user, q)
	case "scrobble":
	case "jukeboxControl":
		h.jukeboxControl(ctx, w, r, user, q)
		return
	case "star":
		h.handleStar(ctx, w, r, user, q, true)
		return
	case "unstar":
		h.handleStar(ctx, w, r, user, q, false)
		return
	case "getNowPlaying":
		body = map[string]interface{}{
			"nowPlaying": map[string]interface{}{"entry": []interface{}{}},
		}
	case "getStarred":
		body = h.getStarred(ctx, user)
	case "getGenres":
		body = h.getGenres(ctx, user)
	case "getArtistInfo":
		body = h.getArtistInfo(r, ctx, q)
	case "getChatMessages":
		body = map[string]interface{}{
			"chatMessages": map[string]interface{}{},
		}
	case "getInternetRadioStations":
		body = map[string]interface{}{
			"internetRadioStations": map[string]interface{}{},
		}
	case "getAvatar":
		http.Error(w, "no avatar available", http.StatusNotFound)
		return
	default:
		h.respond(w, r, "failed", map[string]interface{}{
			"error": map[string]interface{}{"code": 70, "message": "Unknown action: " + action},
		})
		return
	}

	h.respond(w, r, "ok", body)
}

func (h *Handler) authenticate(r *http.Request, q url.Values) (*domain.User, error) {
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		claims, err := h.jwt.Validate(tokenStr)
		if err == nil {
			return h.userRepo.FindByID(r.Context(), claims.UserID)
		}
	}

	u := q.Get("u")
	p := q.Get("p")
	t := q.Get("t")
	s := q.Get("s")

	if u == "" {
		return nil, fmt.Errorf("missing username")
	}

	user, err := h.userRepo.FindByUsername(r.Context(), u)
	if err != nil {
		return nil, err
	}

	if t != "" && s != "" && p == "" {
		return nil, errTokenNotSupported
	}

	if strings.HasPrefix(p, "enc:") {
		decoded, err := hex.DecodeString(p[4:])
		if err != nil {
			return nil, fmt.Errorf("invalid hex password")
		}
		if !auth.CheckPassword(string(decoded), user.PasswordHash) {
			return nil, fmt.Errorf("invalid password")
		}
	} else if p != "" {
		if !auth.CheckPassword(p, user.PasswordHash) {
			return nil, fmt.Errorf("invalid password")
		}
	}

	return user, nil
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, status string, body map[string]interface{}) {
	resp := map[string]interface{}{
		"status":        status,
		"version":       "1.12.0",
		"type":          "subsonic",
		"serverVersion": "0.1.0",
	}
	for k, v := range body {
		resp[k] = v
	}

	callback := r.URL.Query().Get("callback")
	if callback != "" {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintf(w, "%s(%s)", callback, toJSON(map[string]interface{}{"subsonic-response": resp}))
		return
	}

	format := r.URL.Query().Get("f")
	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"subsonic-response": resp})
		return
	}

	h.respondXML(w, resp)
}

func (h *Handler) respondXML(w http.ResponseWriter, data map[string]interface{}) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>`)

	attrs := []string{"status", "version", "type", "serverVersion"}
	var children []string
	for k := range data {
		isAttr := false
		for _, a := range attrs {
			if k == a {
				isAttr = true
				break
			}
		}
		if !isAttr {
			children = append(children, k)
		}
	}

	fmt.Fprintf(w, `<subsonic-response xmlns="http://subsonic.org/restapi"`)
	for _, a := range attrs {
		if v, ok := data[a]; ok {
			fmt.Fprintf(w, ` %s="%s"`, a, xmlEscape(fmt.Sprintf("%v", v)))
		}
	}

	if len(children) == 0 {
		fmt.Fprint(w, "/>")
		return
	}

	fmt.Fprint(w, ">")
	for _, k := range children {
		writeXMLElement(w, k, data[k])
	}
	fmt.Fprint(w, "</subsonic-response>")
}

func writeXMLElement(w http.ResponseWriter, tag string, val interface{}) {
	if val == nil {
		return
	}
	switch v := val.(type) {
	case map[string]interface{}:
		writeXMLMapElement(w, tag, v)
	case []map[string]interface{}:
		for _, item := range v {
			writeXMLMapElement(w, tag, item)
		}
	case []interface{}:
		for _, item := range v {
			writeXMLElement(w, tag, item)
		}
	default:
		fmt.Fprintf(w, "<%s>%s</%s>", tag, xmlEscape(fmt.Sprintf("%v", v)), tag)
	}
}

func writeXMLMapElement(w http.ResponseWriter, tag string, m map[string]interface{}) {
	var attrs, kids []string
	var text string
	for k, v := range m {
		if k == "#text" {
			text = fmt.Sprintf("%v", v)
			continue
		}
		switch v.(type) {
		case map[string]interface{}, []map[string]interface{}, []interface{}:
			kids = append(kids, k)
		default:
			attrs = append(attrs, k)
		}
	}

	fmt.Fprintf(w, "<%s", tag)
	for _, k := range attrs {
		fmt.Fprintf(w, ` %s="%s"`, k, xmlEscape(fmt.Sprintf("%v", m[k])))
	}

	if len(kids) == 0 && text == "" {
		fmt.Fprint(w, "/>")
		return
	}

	fmt.Fprint(w, ">")
	fmt.Fprint(w, xmlEscape(text))
	for _, k := range kids {
		writeXMLElement(w, k, m[k])
	}
	fmt.Fprintf(w, "</%s>", tag)
}

func (h *Handler) getMusicFolders(ctx context.Context, user *domain.User) map[string]interface{} {
	libs, _ := h.libRepo.FindByUserID(ctx, user.ID)
	var folders []map[string]interface{}
	for _, lib := range libs {
		folders = append(folders, map[string]interface{}{
			"id":   lib.ID,
			"name": lib.Name,
		})
	}
	return map[string]interface{}{
		"musicFolders": map[string]interface{}{"musicFolder": folders},
	}
}

func (h *Handler) getArtists(ctx context.Context, user *domain.User) map[string]interface{} {
	libs, _ := h.libRepo.FindByUserID(ctx, user.ID)
	indexMap := make(map[string][]map[string]interface{})

	for _, lib := range libs {
		list, _ := h.artistRepo.FindByLibraryID(ctx, lib.ID)
		for _, a := range list {
			entry := map[string]interface{}{
				"id":         a.ID,
				"name":       a.Name,
				"albumCount": a.TrackCount,
			}
			key := "#"
			if len(a.Name) > 0 {
				c := strings.ToUpper(a.Name[:1])
				if c >= "A" && c <= "Z" {
					key = c
				}
			}
			indexMap[key] = append(indexMap[key], entry)
		}
	}

	var indexes []map[string]interface{}
	for _, c := range "#ABCDEFGHIJKLMNOPQRSTUVWXYZ" {
		if entries, ok := indexMap[string(c)]; ok {
			indexes = append(indexes, map[string]interface{}{
				"name":   string(c),
				"artist": entries,
			})
		}
	}

	return map[string]interface{}{
		"artists": map[string]interface{}{"index": indexes},
	}
}

func (h *Handler) getArtist(ctx context.Context, q url.Values) map[string]interface{} {
	id := q.Get("id")
	artist, err := h.artistRepo.FindByID(ctx, id)
	if err != nil {
		return nil
	}

	albums, _ := h.albumRepo.FindByArtistID(ctx, id)
	var albumList []map[string]interface{}
	for _, a := range albums {
		albumList = append(albumList, albumToSub(&a))
	}

	return map[string]interface{}{
		"artist": map[string]interface{}{
			"id":         artist.ID,
			"name":       artist.Name,
			"albumCount": len(albumList),
			"album":      albumList,
		},
	}
}

func (h *Handler) getAlbum(ctx context.Context, q url.Values) map[string]interface{} {
	id := q.Get("id")
	album, err := h.albumRepo.FindByID(ctx, id)
	if err != nil {
		return nil
	}

	tracks, _ := h.trackRepo.FindByAlbumID(ctx, id)
	var songList []map[string]interface{}
	for i := range tracks {
		songList = append(songList, h.enrichTrack(ctx, &tracks[i]))
	}

	a := albumToSub(album)
	a["song"] = songList

	return map[string]interface{}{"album": a}
}

func (h *Handler) getSong(ctx context.Context, q url.Values) map[string]interface{} {
	id := q.Get("id")
	track, err := h.trackRepo.FindByID(ctx, id)
	if err != nil {
		return nil
	}

	return map[string]interface{}{"song": trackToSub(track)}
}

func (h *Handler) getAlbumList(ctx context.Context, user *domain.User, q url.Values) map[string]interface{} {
	libs, _ := h.libRepo.FindByUserID(ctx, user.ID)
	var all []map[string]interface{}
	for _, lib := range libs {
		albums, _ := h.albumRepo.FindByLibraryID(ctx, lib.ID)
		for i := range albums {
			all = append(all, albumToSub(&albums[i]))
		}
	}

	listType := q.Get("type")
	switch listType {
	case "byYear":
		fromYear, _ := strconv.Atoi(q.Get("fromYear"))
		toYear, _ := strconv.Atoi(q.Get("toYear"))
		var filtered []map[string]interface{}
		for _, a := range all {
			y, _ := a["year"].(int)
			if (fromYear == 0 || y >= fromYear) && (toYear == 0 || y <= toYear) {
				filtered = append(filtered, a)
			}
		}
		all = filtered
	case "byGenre":
		genre := q.Get("genre")
		var filtered []map[string]interface{}
		for _, a := range all {
			g, _ := a["genre"].(string)
			for _, part := range strings.Split(g, ",") {
				if strings.EqualFold(strings.TrimSpace(part), genre) {
					filtered = append(filtered, a)
					break
				}
			}
		}
		all = filtered
	case "random":
		rand.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })
	case "newest":
		sort.Slice(all, func(i, j int) bool {
			yi, _ := all[i]["year"].(int)
			yj, _ := all[j]["year"].(int)
			if yi != yj {
				return yi > yj
			}
			ci, _ := all[i]["created"].(string)
			cj, _ := all[j]["created"].(string)
			return ci > cj
		})
	case "alphabeticalByName":
		sort.Slice(all, func(i, j int) bool {
			ni, _ := all[i]["name"].(string)
			nj, _ := all[j]["name"].(string)
			return strings.ToLower(ni) < strings.ToLower(nj)
		})
	case "alphabeticalByArtist":
		artistNames := make(map[string]string)
		for _, a := range all {
			aid, _ := a["artistId"].(string)
			if _, ok := artistNames[aid]; !ok && aid != "" {
				if artist, err := h.artistRepo.FindByID(ctx, aid); err == nil {
					artistNames[aid] = artist.Name
				}
			}
		}
		sort.Slice(all, func(i, j int) bool {
			ai, _ := all[i]["artistId"].(string)
			aj, _ := all[j]["artistId"].(string)
			aname := strings.ToLower(artistNames[ai])
			bname := strings.ToLower(artistNames[aj])
			if aname != bname {
				return aname < bname
			}
			ni, _ := all[i]["name"].(string)
			nj, _ := all[j]["name"].(string)
			return strings.ToLower(ni) < strings.ToLower(nj)
		})
	}

	offset, _ := strconv.Atoi(q.Get("offset"))
	size, _ := strconv.Atoi(q.Get("size"))
	if size <= 0 || size > 500 {
		size = 50
	}
	if offset < 0 {
		offset = 0
	}
	if offset < len(all) {
		all = all[offset:]
	} else {
		all = nil
	}
	if len(all) > size {
		all = all[:size]
	}

	if all == nil {
		all = []map[string]interface{}{}
	}
	return map[string]interface{}{
		"albumList": map[string]interface{}{"album": all},
	}
}

func (h *Handler) enrichTrack(ctx context.Context, track *domain.Track) map[string]interface{} {
	out := trackToSub(track)
	if track.Artist == nil {
		if tas, err := h.trackRepo.LoadTrackArtists(ctx, track.ID); err == nil && len(tas) > 0 {
			out["artist"] = tas[0].Artist.Name
			out["artistId"] = tas[0].ArtistID
		}
	} else {
		out["artistId"] = track.Artist.ID
	}
	if len(track.Albums) > 0 && track.Albums[0].Album != nil {
		out["album"] = track.Albums[0].Album.Title
		out["year"] = track.Albums[0].Album.Year
		out["genre"] = track.Albums[0].Album.Genre
	} else if len(track.Albums) > 0 {
		if a, err := h.albumRepo.FindByID(ctx, track.Albums[0].AlbumID); err == nil {
			out["album"] = a.Title
			out["year"] = a.Year
			out["genre"] = a.Genre
		}
	}
	return out
}

func (h *Handler) search(ctx context.Context, user *domain.User, q url.Values) map[string]interface{} {
	query := strings.ToLower(q.Get("query"))
	libs, _ := h.libRepo.FindByUserID(ctx, user.ID)

	var artists, albums, songs []map[string]interface{}

	for _, lib := range libs {
		alist, _ := h.artistRepo.FindByLibraryID(ctx, lib.ID)
		for _, a := range alist {
			if query == "" || strings.Contains(strings.ToLower(a.Name), query) {
				artists = append(artists, map[string]interface{}{
					"id": a.ID, "name": a.Name,
				})
			}
		}

		blist, _ := h.albumRepo.FindByLibraryID(ctx, lib.ID)
		for i := range blist {
			if query == "" || strings.Contains(strings.ToLower(blist[i].Title), query) {
				albums = append(albums, albumToSub(&blist[i]))
			}
		}

		tlist, _ := h.trackRepo.FindByLibraryID(ctx, lib.ID)
		for i := range tlist {
			if query == "" || strings.Contains(strings.ToLower(tlist[i].Title), query) {
				songs = append(songs, h.enrichTrack(ctx, &tlist[i]))
			}
		}
	}

	count, _ := strconv.Atoi(q.Get("count"))
	if count <= 0 {
		count = 20
	}
	trunc := func(s []map[string]interface{}) []map[string]interface{} {
		if len(s) > count {
			return s[:count]
		}
		return s
	}

	return map[string]interface{}{
		"searchResult2": map[string]interface{}{
			"artist": trunc(artists),
			"album":  trunc(albums),
			"song":   trunc(songs),
		},
	}
}

func (h *Handler) getPlaylists(ctx context.Context, user *domain.User) map[string]interface{} {
	playlists, _ := h.playlistRepo.FindByUserID(ctx, user.ID)
	var list []map[string]interface{}
	for _, p := range playlists {
		list = append(list, map[string]interface{}{
			"id":        p.ID,
			"name":      p.Name,
			"owner":     user.Username,
			"public":    p.IsPublic,
			"songCount": len(p.TrackIDs),
		})
	}
	if list == nil {
		list = []map[string]interface{}{}
	}
	return map[string]interface{}{
		"playlists": map[string]interface{}{"playlist": list},
	}
}

func (h *Handler) getPlaylist(ctx context.Context, user *domain.User, q url.Values) map[string]interface{} {
	id := q.Get("id")
	pl, err := h.playlistRepo.FindByID(ctx, id)
	if err != nil || (pl.OwnerID != user.ID && !pl.IsPublic) {
		return nil
	}
	tracks, _ := h.trackRepo.FindByIDs(ctx, pl.TrackIDs)
	entries := []map[string]interface{}{}
	for _, t := range tracks {
		entries = append(entries, h.enrichTrack(ctx, t))
	}
	return map[string]interface{}{
		"playlist": map[string]interface{}{
			"id":    pl.ID,
			"name":  pl.Name,
			"entry": entries,
		},
	}
}

func (h *Handler) createPlaylist(ctx context.Context, w http.ResponseWriter, r *http.Request, user *domain.User, q url.Values) {
	name := q.Get("name")
	if name == "" {
		if q.Get("playlistId") != "" {
			name = "Imported Playlist"
		} else {
			h.respond(w, r, "failed", map[string]interface{}{
				"error": map[string]interface{}{"code": 0, "message": "missing name"},
			})
			return
		}
	}

	var trackIDs []string
	if ids, ok := q["songId"]; ok {
		trackIDs = ids
	}

	now := time.Now()
	pl := &domain.Playlist{
		ID:        domain.NewID(),
		Name:      name,
		OwnerID:   user.ID,
		IsPublic:  false,
		TrackIDs:  trackIDs,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.playlistRepo.Create(ctx, pl); err != nil {
		h.respond(w, r, "failed", map[string]interface{}{
			"error": map[string]interface{}{"code": 0, "message": "failed to create playlist"},
		})
		return
	}
	h.respond(w, r, "ok", nil)
}

func (h *Handler) deletePlaylist(ctx context.Context, w http.ResponseWriter, r *http.Request, user *domain.User, q url.Values) {
	id := q.Get("id")
	pl, err := h.playlistRepo.FindByID(ctx, id)
	if err != nil || pl.OwnerID != user.ID {
		h.respond(w, r, "failed", map[string]interface{}{
			"error": map[string]interface{}{"code": 70, "message": "playlist not found"},
		})
		return
	}
	if err := h.playlistRepo.Delete(ctx, id); err != nil {
		h.respond(w, r, "failed", map[string]interface{}{
			"error": map[string]interface{}{"code": 0, "message": "failed to delete playlist"},
		})
		return
	}
	h.respond(w, r, "ok", nil)
}

func (h *Handler) updatePlaylist(ctx context.Context, w http.ResponseWriter, r *http.Request, user *domain.User, q url.Values) {
	id := q.Get("playlistId")
	pl, err := h.playlistRepo.FindByID(ctx, id)
	if err != nil || pl.OwnerID != user.ID {
		h.respond(w, r, "failed", map[string]interface{}{
			"error": map[string]interface{}{"code": 70, "message": "playlist not found"},
		})
		return
	}

	if name := q.Get("name"); name != "" {
		pl.Name = name
	}
	if pub := q.Get("public"); pub != "" {
		pl.IsPublic = pub == "true"
	}

	if ids, ok := q["songIdToAdd"]; ok {
		seen := make(map[string]bool)
		for _, tid := range pl.TrackIDs {
			seen[tid] = true
		}
		for _, tid := range ids {
			if !seen[tid] {
				seen[tid] = true
				pl.TrackIDs = append(pl.TrackIDs, tid)
			}
		}
	}

	if indices, ok := q["songIndexToRemove"]; ok {
		remove := make(map[int]bool)
		for _, s := range indices {
			n, _ := strconv.Atoi(s)
			remove[n] = true
		}
		var filtered []string
		for i, tid := range pl.TrackIDs {
			if !remove[i] {
				filtered = append(filtered, tid)
			}
		}
		pl.TrackIDs = filtered
	}

	pl.UpdatedAt = time.Now()
	if err := h.playlistRepo.Update(ctx, pl); err != nil {
		h.respond(w, r, "failed", map[string]interface{}{
			"error": map[string]interface{}{"code": 0, "message": "failed to update playlist"},
		})
		return
	}
	h.respond(w, r, "ok", nil)
}

func (h *Handler) getUser(ctx context.Context, actor *domain.User, q url.Values) map[string]interface{} {
	username := q.Get("username")
	if username != actor.Username && !isAdmin(actor) {
		return nil
	}
	u, err := h.userRepo.FindByUsername(ctx, username)
	if err != nil {
		return nil
	}
	result := userToSub(u)
	result["jukebox"] = h.hasJukebox(ctx)
	return map[string]interface{}{
		"user": result,
	}
}

func (h *Handler) getUsers(ctx context.Context, user *domain.User) map[string]interface{} {
	if !isAdmin(user) {
		return nil
	}
	users, _ := h.userRepo.ListAll(ctx)
	var list []map[string]interface{}
	hasJb := h.hasJukebox(ctx)
	for _, u := range users {
		entry := userToSub(&u)
		entry["jukebox"] = hasJb
		list = append(list, entry)
	}
	return map[string]interface{}{
		"users": map[string]interface{}{"user": list},
	}
}

func (h *Handler) hasJukebox(ctx context.Context) bool {
	var val string
	err := h.db.QueryRowContext(ctx, "SELECT value FROM server_settings WHERE key='subsonic_jukebox_id'").Scan(&val)
	return err == nil && val != ""
}

func (h *Handler) jukeboxControl(ctx context.Context, w http.ResponseWriter, r *http.Request, user *domain.User, q url.Values) {
	var jukeboxID string
	h.db.QueryRowContext(ctx, "SELECT value FROM server_settings WHERE key='subsonic_jukebox_id'").Scan(&jukeboxID)
	if jukeboxID == "" || h.engineManager == nil {
		h.respond(w, r, "failed", map[string]interface{}{
			"error": map[string]interface{}{"code": 50, "message": "Jukebox is not configured"},
		})
		return
	}
	if !isAdmin(user) {
		h.respond(w, r, "failed", map[string]interface{}{
			"error": map[string]interface{}{"code": 50, "message": "User is not authorized for jukebox playback"},
		})
		return
	}

	engine, _ := h.engineManager.GetOrCreate(jukeboxID, "", "", h.resolveTrack)
	action := q.Get("action")
	switch action {
	case "status":
		st := engine.Status()
		h.respond(w, r, "ok", map[string]interface{}{
			"jukeboxStatus": map[string]interface{}{
				"position":     int(st.Position),
				"currentIndex": st.QueueIdx,
				"playing":      st.State == player.StatePlaying,
				"gain":         st.Volume,
			},
		})
	case "start":
		st := engine.Status()
		if st.State == player.StateStopped && st.QueueIdx < len(st.Queue) {
			id := st.Queue[st.QueueIdx]
			info, err := h.resolveTrack(id)
			if err == nil {
				engine.Play(id, info)
			}
		}
		h.respond(w, r, "ok", map[string]interface{}{
			"jukeboxStatus": map[string]interface{}{},
		})
	case "stop":
		engine.Stop()
		h.respond(w, r, "ok", map[string]interface{}{
			"jukeboxStatus": map[string]interface{}{},
		})
	case "set":
		if ids, ok := q["id"]; ok {
			engine.SetQueue(ids)
		}
		h.respond(w, r, "ok", map[string]interface{}{
			"jukeboxStatus": map[string]interface{}{},
		})
	case "skip":
		index, _ := strconv.Atoi(q.Get("index"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		_ = offset
		st := engine.Status()
		if index >= 0 && index < len(st.Queue) {
			engine.SetQueue(st.Queue)
			id := st.Queue[index]
			info, err := h.resolveTrack(id)
			if err == nil {
				engine.Play(id, info)
			}
		}
		h.respond(w, r, "ok", map[string]interface{}{
			"jukeboxStatus": map[string]interface{}{},
		})
	case "setGain":
		gain, _ := strconv.ParseFloat(q.Get("gain"), 64)
		engine.SetVolume(gain)
		h.respond(w, r, "ok", map[string]interface{}{
			"jukeboxStatus": map[string]interface{}{},
		})
	default:
		h.respond(w, r, "failed", map[string]interface{}{
			"error": map[string]interface{}{"code": 0, "message": "unknown jukebox action"},
		})
	}
}

func (h *Handler) resolveTrack(id string) (*player.TrackInfo, error) {
	track, err := h.trackRepo.FindByID(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &player.TrackInfo{
		ID:        track.ID,
		Title:     track.Title,
		FilePath:  track.FilePath,
		LibraryID: track.LibraryID,
		Duration:  track.Duration,
	}, nil
}

func (h *Handler) createUser(ctx context.Context, actor *domain.User, q url.Values) map[string]interface{} {
	if !isAdmin(actor) {
		return nil
	}
	username := q.Get("username")
	email := q.Get("email")
	password := q.Get("password")
	if username == "" || email == "" || password == "" {
		return nil
	}
	hash, _ := auth.HashPassword(password)
	now := time.Now()
	role := domain.RoleUser
	if "true" == q.Get("admin") {
		role = domain.RoleAdmin
	}
	u := &domain.User{
		ID:           domain.NewID(),
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		Role:         role,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := h.userRepo.Create(ctx, u); err != nil {
		return nil
	}
	return nil
}

func (h *Handler) updateUser(ctx context.Context, actor *domain.User, q url.Values) map[string]interface{} {
	if !isAdmin(actor) {
		return nil
	}
	username := q.Get("username")
	target, err := h.userRepo.FindByUsername(ctx, username)
	if err != nil {
		return nil
	}
	if target.Role == domain.RoleSuperAdmin {
		return nil
	}
	if email := q.Get("email"); email != "" {
		target.Email = email
	}
	if admin := q.Get("admin"); admin != "" {
		if admin == "true" {
			target.Role = domain.RoleAdmin
		} else {
			target.Role = domain.RoleUser
		}
	}
	target.UpdatedAt = time.Now()
	if err := h.userRepo.Update(ctx, target); err != nil {
		return nil
	}
	return nil
}

func (h *Handler) deleteUser(ctx context.Context, actor *domain.User, q url.Values) map[string]interface{} {
	if !isAdmin(actor) {
		return nil
	}
	username := q.Get("username")
	target, err := h.userRepo.FindByUsername(ctx, username)
	if err != nil {
		return nil
	}
	if target.Role == domain.RoleSuperAdmin {
		return nil
	}
	_ = h.userRepo.Delete(ctx, target.ID)
	return nil
}

func (h *Handler) changePassword(ctx context.Context, actor *domain.User, q url.Values) map[string]interface{} {
	if !isAdmin(actor) {
		return nil
	}
	username := q.Get("username")
	password := q.Get("password")
	target, err := h.userRepo.FindByUsername(ctx, username)
	if err != nil || password == "" || target.Role == domain.RoleSuperAdmin {
		return nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return nil
	}
	target.PasswordHash = hash
	target.UpdatedAt = time.Now()
	if err := h.userRepo.Update(ctx, target); err != nil {
		return nil
	}
	return nil
}

func isAdmin(u *domain.User) bool {
	return u.Role == domain.RoleSuperAdmin || u.Role == domain.RoleAdmin
}

func userToSub(u *domain.User) map[string]interface{} {
	isAdm := u.Role == domain.RoleSuperAdmin || u.Role == domain.RoleAdmin
	return map[string]interface{}{
		"username":        u.Username,
		"email":           u.Email,
		"admin":           isAdm,
		"scrobbling":      true,
		"download":        true,
		"upload":          isAdm,
		"playlist":        true,
		"coverArt":        true,
		"comment":         false,
		"podcast":         false,
		"share":           false,
		"videoConversion": false,
		"lastfm":          false,
	}
}

func (h *Handler) getMusicDirectory(ctx context.Context, user *domain.User, q url.Values) map[string]interface{} {
	id := q.Get("id")
	if id == "" {
		return nil
	}

	libs, _ := h.libRepo.FindByUserID(ctx, user.ID)
	libSet := make(map[string]bool)
	for _, lib := range libs {
		libSet[lib.ID] = true
	}

	if album, err := h.albumRepo.FindByID(ctx, id); err == nil {
		tracks, _ := h.trackRepo.FindByAlbumID(ctx, id)
		children := []map[string]interface{}{}
		for i := range tracks {
			if !libSet[tracks[i].LibraryID] {
				continue
			}
			child := h.enrichTrack(ctx, &tracks[i])
			child["parent"] = album.ID
			children = append(children, child)
		}
		return map[string]interface{}{
			"directory": map[string]interface{}{
				"id":     album.ID,
				"name":   album.Title,
				"parent": album.ArtistID,
				"child":  children,
			},
		}
	}

	if artist, err := h.artistRepo.FindByID(ctx, id); err == nil {
		albums, _ := h.albumRepo.FindByArtistID(ctx, id)
		children := []map[string]interface{}{}
		for i := range albums {
			var libID string
			h.db.QueryRowContext(ctx,
				"SELECT DISTINCT t.library_id FROM tracks t JOIN track_albums ta ON ta.track_id = t.id WHERE ta.album_id = $1 LIMIT 1",
				albums[i].ID).Scan(&libID)
			if !libSet[libID] {
				continue
			}
			child := albumToSub(&albums[i])
			child["isDir"] = true
			child["title"] = albums[i].Title
			child["parent"] = artist.ID
			children = append(children, child)
		}
		return map[string]interface{}{
			"directory": map[string]interface{}{
				"id":    artist.ID,
				"name":  artist.Name,
				"child": children,
			},
		}
	}

	return nil
}

func (h *Handler) getStarred(ctx context.Context, user *domain.User) map[string]interface{} {
	rows, err := h.db.QueryContext(ctx,
		"SELECT item_type, item_id FROM favorites WHERE user_id = $1 ORDER BY created_at DESC", user.ID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var songs, albums, artists []map[string]interface{}
	for rows.Next() {
		var itemType, itemID string
		if err := rows.Scan(&itemType, &itemID); err != nil {
			continue
		}
		switch itemType {
		case "track":
			track, err := h.trackRepo.FindByID(ctx, itemID)
			if err != nil {
				continue
			}
			songs = append(songs, h.enrichTrack(ctx, track))
		case "album":
			album, err := h.albumRepo.FindByID(ctx, itemID)
			if err != nil {
				continue
			}
			entry := albumToSub(album)
			entry["isDir"] = true
			albums = append(albums, entry)
		case "artist":
			artist, err := h.artistRepo.FindByID(ctx, itemID)
			if err != nil {
				continue
			}
			artists = append(artists, map[string]interface{}{
				"id":   artist.ID,
				"name": artist.Name,
			})
		}
	}
	if err := rows.Err(); err != nil {
		logger.Error("[subsonic] starred query error: %v", err)
	}

	if songs == nil {
		songs = []map[string]interface{}{}
	}
	if albums == nil {
		albums = []map[string]interface{}{}
	}
	if artists == nil {
		artists = []map[string]interface{}{}
	}

	return map[string]interface{}{
		"starred": map[string]interface{}{
			"song":   songs,
			"album":  albums,
			"artist": artists,
		},
	}
}

func (h *Handler) getGenres(ctx context.Context, user *domain.User) map[string]interface{} {
	libs, _ := h.libRepo.FindByUserID(ctx, user.ID)
	libIDs := make([]string, len(libs))
	for i, lib := range libs {
		libIDs[i] = lib.ID
	}
	if len(libIDs) == 0 {
		return map[string]interface{}{
			"genres": map[string]interface{}{"genre": []interface{}{}},
		}
	}

	rows, err := h.db.QueryContext(ctx,
		`SELECT a.genre, COUNT(DISTINCT a.id) AS album_count, COUNT(DISTINCT t.id) AS song_count
		 FROM albums a
		 JOIN track_albums ta ON ta.album_id = a.id
		 JOIN tracks t ON t.id = ta.track_id
		 WHERE t.library_id = ANY($1) AND a.genre != ''
		 GROUP BY a.genre ORDER BY a.genre`, pq.Array(libIDs))
	if err != nil {
		return nil
	}
	defer rows.Close()

	var genres []map[string]interface{}
	for rows.Next() {
		var name string
		var albumCount, songCount int
		if err := rows.Scan(&name, &albumCount, &songCount); err != nil {
			continue
		}
		genres = append(genres, map[string]interface{}{
			"#text":      name,
			"songCount":  songCount,
			"albumCount": albumCount,
		})
	}
	if err := rows.Err(); err != nil {
		logger.Error("[subsonic] genres query error: %v", err)
	}
	if genres == nil {
		genres = []map[string]interface{}{}
	}
	return map[string]interface{}{
		"genres": map[string]interface{}{"genre": genres},
	}
}

func (h *Handler) getArtistInfo(r *http.Request, ctx context.Context, q url.Values) map[string]interface{} {
	id := q.Get("id")
	artist, err := h.artistRepo.FindByID(ctx, id)
	if err != nil {
		return nil
	}

	var bio, mbid, img map[string]interface{}
	if artist.Biography != "" {
		bio = map[string]interface{}{"#text": artist.Biography}
	}
	// musicBrainzId is expected by clients to be an MBID; only emit it for
	// MusicBrainz-sourced (or legacy source-less) artists. For other sources
	// fall back to a MusicBrainz alias in external_ids when present.
	musicBrainzID := ""
	if artist.MetadataSource == "" || artist.MetadataSource == "musicbrainz" {
		musicBrainzID = artist.ExternalID
	} else if artist.ExternalIDs != nil {
		musicBrainzID = artist.ExternalIDs["musicbrainz"]
	}
	if musicBrainzID != "" {
		mbid = map[string]interface{}{"#text": musicBrainzID}
	}
	if artist.CoverImageID != nil && *artist.CoverImageID != "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		img = map[string]interface{}{
			"#text": fmt.Sprintf("%s://%s/rest/getCoverArt?id=%s", scheme, r.Host, *artist.CoverImageID),
		}
	}
	info := map[string]interface{}{}
	if bio != nil {
		info["biography"] = bio
	}
	if mbid != nil {
		info["musicBrainzId"] = mbid
	}
	if img != nil {
		info["largeImageUrl"] = img
	}
	return map[string]interface{}{
		"artistInfo": info,
	}
}

func (h *Handler) handleStar(ctx context.Context, w http.ResponseWriter, r *http.Request, user *domain.User, q url.Values, add bool) {
	process := func(itemType string, ids []string) {
		for _, itemID := range ids {
			var libID sql.NullString
			switch itemType {
			case "track":
				h.db.QueryRowContext(ctx, "SELECT library_id FROM tracks WHERE id = $1", itemID).Scan(&libID)
			case "album":
				h.db.QueryRowContext(ctx,
					"SELECT DISTINCT t.library_id FROM tracks t JOIN track_albums ta ON ta.track_id = t.id WHERE ta.album_id = $1 LIMIT 1",
					itemID).Scan(&libID)
			case "artist":
				h.db.QueryRowContext(ctx,
					"SELECT DISTINCT t.library_id FROM tracks t JOIN track_artists ta ON ta.track_id = t.id WHERE ta.artist_id = $1 LIMIT 1",
					itemID).Scan(&libID)
			}
			if add {
				if _, err := h.db.ExecContext(ctx,
					"INSERT INTO favorites (user_id, item_type, item_id, library_id, created_at) VALUES ($1,$2,$3,$4,NOW()) ON CONFLICT DO NOTHING",
					user.ID, itemType, itemID, libID); err != nil {
					logger.Error("[subsonic] star insert error: %v", err)
				}
			} else {
				if _, err := h.db.ExecContext(ctx,
					"DELETE FROM favorites WHERE user_id = $1 AND item_type = $2 AND item_id = $3",
					user.ID, itemType, itemID); err != nil {
					logger.Error("[subsonic] unstar delete error: %v", err)
				}
			}
		}
	}

	if ids, ok := q["id"]; ok {
		process("track", ids)
	} else if ids, ok := q["albumId"]; ok {
		process("album", ids)
	} else if ids, ok := q["artistId"]; ok {
		process("artist", ids)
	}

	h.respond(w, r, "ok", nil)
}

func (h *Handler) serveStream(w http.ResponseWriter, r *http.Request, q url.Values) {
	id := q.Get("id")
	track, err := h.trackRepo.FindByID(r.Context(), id)
	if err != nil {
		http.Error(w, "track not found", http.StatusNotFound)
		return
	}
	quality := transcoder.ParseQuality(r.URL.Query().Get("quality"))
	if transcoder.Decide(track.BitRate, track.AudioCodec, quality).Transcode {
		transcoder.ServeTranscoded(r.Context(), w, r, track.FilePath, quality)
		return
	}
	w.Header().Set("Content-Type", "audio/"+track.FileFormat)
	w.Header().Set("Content-Length", strconv.FormatInt(track.FileSize, 10))
	http.ServeFile(w, r, track.FilePath)
}

// serveCoverArt resolves the id as an images-row id and serves the stored
// variant. Unknown ids are a hard 404 — clients are expected to pass the
// coverArt id they received from the API.
func (h *Handler) serveCoverArt(w http.ResponseWriter, r *http.Request, q url.Values) {
	id := q.Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	img, err := h.images.FindByID(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "cover art not found", http.StatusNotFound)
		return
	}
	if err != nil {
		logger.Error("[subsonic] cover art lookup error: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// getCoverArt accepts an optional size; honor it via the variant picker
	// (default 256, then the original for small sources).
	size := 256
	if s := q.Get("size"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v > 0 {
			size = v
		}
	}
	p := metadata.ImageVariantPath(img, size)
	if !fileExists(p) {
		p = img.Path
	}
	if !fileExists(p) {
		http.Error(w, "cover art not found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, p)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func albumToSub(a *domain.Album) map[string]interface{} {
	out := map[string]interface{}{
		"id":        a.ID,
		"name":      a.Title,
		"title":     a.Title,
		"artist":    "",
		"artistId":  a.ArtistID,
		"year":      a.Year,
		"genre":     a.Genre,
		"songCount": a.SongCount,
		"duration":  a.Duration,
		"created":   a.CreatedAt.Format(time.RFC3339),
	}
	if a.CoverImageID != nil {
		out["coverArt"] = *a.CoverImageID
	}
	return out
}

func trackToSub(t *domain.Track) map[string]interface{} {
	albumID := ""
	trackNum := 0
	discNum := 0
	albumTitle := ""
	if len(t.Albums) > 0 {
		albumID = t.Albums[0].AlbumID
		trackNum = t.Albums[0].TrackNumber
		discNum = t.Albums[0].DiscNumber
		if t.Albums[0].Album != nil {
			albumTitle = t.Albums[0].Album.Title
		}
	}
	out := map[string]interface{}{
		"id":                    t.ID,
		"title":                 t.Title,
		"artist":                "",
		"album":                 albumTitle,
		"albumId":               albumID,
		"track":                 trackNum,
		"discNumber":            discNum,
		"duration":              t.Duration,
		"bitRate":               t.BitRate / 1000,
		"size":                  t.FileSize,
		"suffix":                t.FileFormat,
		"contentType":           "audio/" + t.FileFormat,
		"transcodedContentType": "audio/" + t.FileFormat,
		"transcodedSuffix":      t.FileFormat,
		"path":                  t.FilePath,
		"isDir":                 false,
		"type":                  "music",
	}
	if t.CoverImageID != nil {
		out["coverArt"] = *t.CoverImageID
	}
	if t.Artist != nil {
		out["artist"] = t.Artist.Name
	}
	return out
}

func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	return s
}
