package subsonic

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sonicore/server/internal/core/domain"
	"github.com/sonicore/server/internal/core/service"
	"github.com/sonicore/server/internal/infrastructure/auth"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/repository"
)

type Handler struct {
	db           *sql.DB
	jwt          *auth.JWTService
	userRepo     *repository.UserRepo
	trackRepo    *repository.TrackRepo
	albumRepo    *repository.AlbumRepo
	artistRepo   *repository.ArtistRepo
	libRepo      *repository.LibraryRepo
	playlistRepo *repository.PlaylistRepo
	scanner      *service.ScannerService
	imagesDir    string
}

func NewHandler(db *sql.DB, jwt *auth.JWTService, scanner *service.ScannerService, imagesDir string) *Handler {
	return &Handler{
		db:           db,
		jwt:          jwt,
		userRepo:     repository.NewUserRepo(db),
		imagesDir:    imagesDir,
		trackRepo:    repository.NewTrackRepo(db),
		albumRepo:    repository.NewAlbumRepo(db),
		artistRepo:   repository.NewArtistRepo(db),
		libRepo:      repository.NewLibraryRepo(db),
		playlistRepo: repository.NewPlaylistRepo(db),
		scanner:      scanner,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	action := strings.TrimPrefix(r.URL.Path, "/rest/")
	action = strings.Split(action, ".")[0]
	action = strings.Split(action, "/")[0]

	user, err := h.authenticate(r, q)
	if err != nil {
		log.Printf("[subsonic] auth failed: %v", err)
		h.respond(w, r, "failed", map[string]interface{}{
			"error": map[string]interface{}{
				"code":    40,
				"message": "Wrong username or password",
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
				log.Printf("[subsonic] start scan error: %v", err)
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
	case "scrobble":
	case "getNowPlaying":
		body = map[string]interface{}{
			"nowPlaying": map[string]interface{}{"entry": []interface{}{}},
		}
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

	if t != "" && s != "" {
		expected := fmt.Sprintf("%x", md5.Sum([]byte(user.PasswordHash+s)))
		if t != expected {
			return nil, fmt.Errorf("invalid token")
		}
		return user, nil
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
		"version":       "1.16.1",
		"type":          "sonicore",
		"serverVersion": "0.1.0",
	}
	for k, v := range body {
		resp[k] = v
	}

	wrapper := map[string]interface{}{"subsonic-response": resp}

	callback := r.URL.Query().Get("callback")
	if callback != "" {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprintf(w, "%s(%s)", callback, toJSON(wrapper))
	} else {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(wrapper)
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

	size, _ := strconv.Atoi(q.Get("size"))
	if size <= 0 || size > 500 {
		size = 50
	}
	if len(all) > size {
		all = all[:size]
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
	} else if len(track.Albums) > 0 {
		if a, err := h.albumRepo.FindByID(ctx, track.Albums[0].AlbumID); err == nil {
			out["album"] = a.Title
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

func (h *Handler) serveStream(w http.ResponseWriter, r *http.Request, q url.Values) {
	id := q.Get("id")
	track, err := h.trackRepo.FindByID(r.Context(), id)
	if err != nil {
		http.Error(w, "track not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "audio/"+track.FileFormat)
	w.Header().Set("Content-Length", strconv.FormatInt(track.FileSize, 10))
	http.ServeFile(w, r, track.FilePath)
}

func (h *Handler) serveCoverArt(w http.ResponseWriter, r *http.Request, q url.Values) {
	id := q.Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	coverPath := func(libID, ownerType, ownerID string) string {
		for _, s := range []int{64, 256} {
			p := metadata.CoverPathWithSuffix(h.imagesDir, libID, ownerType, ownerID, fmt.Sprintf("_%d", s), "jpg")
			if fileExists(p) {
				return p
			}
		}
		return metadata.CoverPathWithSuffix(h.imagesDir, libID, ownerType, ownerID, "", "jpg")
	}

	ctx := r.Context()

	track, err := h.trackRepo.FindByID(ctx, id)
	if err == nil && track.CoverImageID != nil {
		imagePath := coverPath(track.LibraryID, "track", track.ID)
		if _, err := os.Stat(imagePath); err == nil {
			http.ServeFile(w, r, imagePath)
			return
		}
	}

	var album *domain.Album
	if track != nil && len(track.Albums) > 0 {
		album, _ = h.albumRepo.FindByID(ctx, track.Albums[0].AlbumID)
	} else {
		album, _ = h.albumRepo.FindByID(ctx, id)
	}
	if album != nil && album.CoverImageID != nil {
		imagePath := coverPath("album", "album", album.ID)
		if _, err := os.Stat(imagePath); err == nil {
			http.ServeFile(w, r, imagePath)
			return
		}
		if albumTracks, err := h.trackRepo.FindByAlbumID(ctx, album.ID); err == nil {
			for i := range albumTracks {
				if albumTracks[i].CoverImageID != nil {
					if p := coverPath("album", "track", albumTracks[i].ID); fileExists(p) {
						http.ServeFile(w, r, p)
						return
					}
				}
			}
		}
	}

	artist, err := h.artistRepo.FindByID(ctx, id)
	if err == nil && artist.CoverImageID != nil {
		imagePath := coverPath("artist", "artist", artist.ID)
		if _, err := os.Stat(imagePath); err == nil {
			http.ServeFile(w, r, imagePath)
			return
		}
	}

	http.Error(w, "cover art not found", http.StatusNotFound)
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
		"id":          t.ID,
		"title":       t.Title,
		"artist":      "",
		"album":       albumTitle,
		"albumId":     albumID,
		"track":       trackNum,
		"discNumber":  discNum,
		"duration":    t.Duration,
		"bitRate":     t.BitRate / 1000,
		"size":        t.FileSize,
		"suffix":      t.FileFormat,
		"contentType": "audio/" + t.FileFormat,
		"type":        "music",
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
