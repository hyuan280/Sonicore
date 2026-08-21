package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"

	"github.com/sonicore/server/internal/api/middleware"
	"github.com/sonicore/server/internal/api/rest"
	"github.com/sonicore/server/internal/api/subsonic"
	"github.com/sonicore/server/internal/api/ws"
	"github.com/sonicore/server/internal/config"
	"github.com/sonicore/server/internal/core/port"
	"github.com/sonicore/server/internal/core/service"
	"github.com/sonicore/server/internal/infrastructure/auth"
	"github.com/sonicore/server/internal/infrastructure/cache"
	"github.com/sonicore/server/internal/infrastructure/download"
	"github.com/sonicore/server/internal/infrastructure/external/netease"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/infrastructure/lyrics"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/player"
	"github.com/sonicore/server/internal/infrastructure/repository"
	"github.com/sonicore/server/internal/infrastructure/secrets"
	"github.com/sonicore/server/internal/infrastructure/transcoder"
)

type Server struct {
	cfg    *config.Config
	db     *sql.DB
	vk     *cache.Valkey
	router *mux.Router
	http   *http.Server
}

func New(cfg *config.Config) (*Server, error) {
	db, err := repository.NewDB(cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}

	if err := repository.RunMigrations(db); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	if err := cfg.InitDirs(); err != nil {
		return nil, fmt.Errorf("init dirs: %w", err)
	}
	if err := transcoder.Init(cfg.Data.CacheDir); err != nil {
		return nil, fmt.Errorf("transcoder init: %w", err)
	}

	vk := cache.NewValkey(cfg.Redis)
	if err := vk.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("valkey ping failed: %w", err)
	}
	log.Println("[cache] connected to Valkey")

	jwtService := auth.NewJWTService(cfg.JWT.Secret, cfg.JWT.Expiration)
	tokenStore := cache.NewTokenStore(vk)
	sessionStore := cache.NewSessionStore(vk)

	refreshExp, err := time.ParseDuration(cfg.JWT.RefreshExpiration)
	if err != nil {
		refreshExp = 720 * time.Hour
	}

	engineManager := player.NewEngineManager()

	pulseServer := cfg.Audio.PulseServer
	if pulseServer == "" {
		pulseServer = os.Getenv("SONICORE_AUDIO_PULSE_SERVER")
	}
	if pulseServer != "" {
		os.Setenv("PULSE_SERVER", pulseServer)
		log.Printf("[audio] pulse server: %s", pulseServer)
	}

	mbCfg := metadata.MBConfig{
		Enabled:   cfg.Metadata.MusicBrainzEnabled,
		APIURL:    cfg.Metadata.MusicBrainzAPIURL,
		RateLimit: cfg.Metadata.MusicBrainzRateLimit,
		AppName:   cfg.Metadata.MusicBrainzAppName,
		AppVer:    cfg.Metadata.MusicBrainzAppVersion,
	}
	// One shared MusicBrainz client for every request path (cover restore,
	// scans, manual identify): a single rate limiter and HTTP client instead
	// of a fresh instance per registry build. Runtime settings propagate via
	// ApplyConfig (MBConfig.Client).
	mbClient := metadata.NewMBClient(mbCfg)
	platformProviders, neteaseProvider := buildPlatformProviders(cfg)
	// Runtime settings (netEase cookie, metadata switches) are read through a
	// short-TTL cache so the per-request / per-cover lookups do not hit the
	// database on every outbound API call; admin changes still apply within
	// the TTL window.
	settingsRepo := repository.NewSettingsRepo(db)
	cachedSettings := newCachedSettings()
	// At-rest encryption for platform credentials stored in the settings DB,
	// keyed off the master (JWT) secret via HKDF. A weak (short) secret is a
	// startup error, not a panic: the caller surfaces an actionable message
	// and the process exits cleanly.
	enc, err := secrets.New([]byte(cfg.JWT.Secret))
	if err != nil {
		return nil, err
	}
	// Wire the runtime NetEase cookie (admin settings) into the shared
	// provider so changes apply without a restart. The stored value is
	// encrypted at rest; decrypt it at the point of use.
	neteaseProvider.SetCookieProvider(func() string {
		raw := cachedSettings.get(settingsRepo, "platforms_netease_cookie")
		if raw == "" {
			return ""
		}
		dec, err := enc.Decrypt(raw)
		if err != nil {
			log.Printf("[server] decrypt netease cookie: %v", err)
			return ""
		}
		return dec
	})
	// The NetEase request pacing follows the runtime admin setting
	// (platforms_netease_rate_limit), falling back to the config value; an
	// empty/invalid setting keeps the config default.
	neteaseProvider.SetRateLimitProvider(func() int {
		raw := cachedSettings.get(settingsRepo, "platforms_netease_rate_limit")
		if n, err := strconv.Atoi(raw); err == nil {
			return n
		}
		return cfg.Platforms.Netease.RateLimit
	})
	// Runtime log level override from admin settings.
	if lvl := cachedSettings.get(settingsRepo, "log_level"); lvl != "" {
		if _, ok := logger.ParseLevelOk(lvl); !ok {
			log.Printf("[server] invalid log_level %q in settings, ignoring", lvl)
		} else {
			logger.SetLevel(lvl)
		}
	}
	// One cover manager shared by the scanner and the HTTP cover handlers so
	// extraction is serialized across both paths. Its platform lookup builds
	// the registry from the same switches the scanner uses.
	umRepo := repository.NewUserMetadataRepo(db)
	buildRegistry := func() *metadata.Registry {
		mbCfg := mbCfg
		mbCfg.Client = mbClient
		// Read every runtime switch in one batched, TTL-cached query so a cold
		// cache (or a failing DB) does not serialize one timeout per key on
		// the cover hot path.
		vals := cachedSettings.getMany(settingsRepo,
			"metadata_musicbrainz_enabled", "metadata_musicbrainz_api_url",
			"metadata_musicbrainz_rate_limit", "metadata_netease_enabled")
		if enabled := vals["metadata_musicbrainz_enabled"]; enabled != "" {
			mbCfg.Enabled = enabled == "true"
		}
		if url := vals["metadata_musicbrainz_api_url"]; url != "" {
			mbCfg.APIURL = url
		}
		if rl := vals["metadata_musicbrainz_rate_limit"]; rl != "" {
			if n, err := strconv.Atoi(rl); err != nil || n <= 0 {
				log.Printf("[server] invalid musicbrainz rate limit %q", rl)
			} else {
				mbCfg.RateLimit = n
			}
		}
		neEnabled := cfg.Metadata.NeteaseEnabled
		if enabled := vals["metadata_netease_enabled"]; enabled != "" {
			neEnabled = enabled == "true"
		}
		return metadata.BuildRegistry(mbCfg, neteaseProvider, neEnabled, umRepo)
	}
	covers := metadata.NewCoverManager(cfg.Data.ImagesDir, db, buildRegistry)
	scannerService := service.NewScannerService(db, cfg.Data.ImagesDir, cfg.Data.LyricsDir, mbCfg, mbClient, neteaseProvider, cfg.Metadata.NeteaseEnabled, covers)
	downloadManager := download.NewManager(db)
	wsHub := ws.NewHub()

	router := mux.NewRouter()
	middleware.SetTrustedProxies(cfg.Server.TrustedProxies)
	registerRoutes(router, db, jwtService, tokenStore, sessionStore, scannerService, downloadManager, engineManager, wsHub, refreshExp, cfg, platformProviders, neteaseProvider, covers, enc, mbClient)

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpSrv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return &Server{
		cfg:    cfg,
		db:     db,
		vk:     vk,
		router: router,
		http:   httpSrv,
	}, nil
}

func registerRoutes(r *mux.Router, db *sql.DB, jwtService *auth.JWTService, tokenStore *cache.TokenStore, sessionStore *cache.SessionStore, scannerService *service.ScannerService, downloadManager *download.Manager, engineManager *player.EngineManager, wsHub *ws.Hub, refreshExp time.Duration, cfg *config.Config, platformProviders map[string]port.PlatformProvider, neteaseProvider *netease.Provider, covers *metadata.CoverManager, enc *secrets.Encryptor, mbClient *metadata.MBClient) {
	r.Use(corsMiddleware)
	r.Use(loggingMiddleware)

	r.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("pong"))
	}).Methods("GET")

	subsonicHandler := subsonic.NewHandler(db, jwtService, scannerService, engineManager)
	r.PathPrefix("/rest").Handler(subsonicHandler)

	api := r.PathPrefix("/api").Subrouter()

	healthHandler := rest.NewHealthHandler(db)
	api.Handle("/health", healthHandler).Methods("GET")

	// Public: check registration status
	api.HandleFunc("/auth/registration-status", func(w http.ResponseWriter, r *http.Request) {
		settingsRepo := repository.NewSettingsRepo(db)
		allowReg, _ := settingsRepo.Get(r.Context(), "allow_registration")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"allow_registration": allowReg == "true",
		})
	}).Methods("GET")

	authLimiter := middleware.NewRateLimiter(10, time.Minute)
	authRateLimit := middleware.RateLimitMiddleware(authLimiter)

	authHandler := rest.NewAuthHandler(db, jwtService, tokenStore, sessionStore, refreshExp)
	authR := api.PathPrefix("/auth").Subrouter()
	authR.Use(authRateLimit)
	authR.HandleFunc("/register", authHandler.Register).Methods("POST")
	authR.HandleFunc("/login", authHandler.Login).Methods("POST")
	authR.HandleFunc("/refresh", authHandler.Refresh).Methods("POST")

	protected := r.PathPrefix("/api").Subrouter()
	protected.Use(middleware.AuthMiddleware(jwtService))

	protected.HandleFunc("/auth/logout", authHandler.Logout).Methods("POST")

	libHandler := rest.NewLibraryHandler(db, cfg.Data.ImagesDir, cfg.Data.LyricsDir, covers, engineManager)
	protected.HandleFunc("/libraries", libHandler.Create).Methods("POST")
	protected.HandleFunc("/libraries", libHandler.List).Methods("GET")
	protected.HandleFunc("/libraries/{id}", libHandler.Get).Methods("GET")
	protected.HandleFunc("/libraries/{id}", libHandler.Delete).Methods("DELETE")
	protected.HandleFunc("/libraries/{id}/members", libHandler.ListMembers).Methods("GET")
	protected.HandleFunc("/libraries/{id}/members", libHandler.AddMember).Methods("POST")
	protected.HandleFunc("/libraries/{id}/members/{userId}", libHandler.RemoveMember).Methods("DELETE")
	protected.HandleFunc("/libraries/{id}/members/{userId}", libHandler.UpdateMemberRole).Methods("PUT")

	scanHandler := rest.NewScanHandler(db, scannerService)
	protected.HandleFunc("/libraries/{id}/scan", scanHandler.Start).Methods("POST")
	protected.HandleFunc("/libraries/{id}/scan/status", scanHandler.Status).Methods("GET")

	metadataHandler := rest.NewMetadataHandler(db, metadata.MBConfig{
		Enabled:   cfg.Metadata.MusicBrainzEnabled,
		APIURL:    cfg.Metadata.MusicBrainzAPIURL,
		RateLimit: cfg.Metadata.MusicBrainzRateLimit,
		AppName:   cfg.Metadata.MusicBrainzAppName,
		AppVer:    cfg.Metadata.MusicBrainzAppVersion,
	}, mbClient, neteaseProvider, cfg.Metadata.NeteaseEnabled, covers)
	protected.HandleFunc("/metadata/identify", metadataHandler.Identify).Methods("POST")
	protected.HandleFunc("/metadata/reidentify", metadataHandler.Reidentify).Methods("POST")
	protected.HandleFunc("/metadata/search/track", metadataHandler.SearchTrack).Methods("POST")
	protected.HandleFunc("/metadata/save", metadataHandler.Save).Methods("POST")
	protected.HandleFunc("/metadata/search/artist", metadataHandler.SearchArtist).Methods("POST")
	protected.HandleFunc("/metadata/search/album", metadataHandler.SearchRelease).Methods("POST")
	protected.HandleFunc("/metadata/sources", metadataHandler.ListSources).Methods("GET")

	userHandler := rest.NewUserHandler(db, sessionStore, tokenStore)
	protected.HandleFunc("/user/me", userHandler.Me).Methods("GET")
	protected.HandleFunc("/user/me", userHandler.MeRenew).Methods("POST")
	protected.HandleFunc("/user/password", userHandler.ChangePassword).Methods("PUT")

	browseHandler := rest.NewDataHandler(db)
	protected.HandleFunc("/data/tracks", browseHandler.Tracks).Methods("GET")
	protected.HandleFunc("/data/tracks/byids", browseHandler.TracksByIDs).Methods("POST")
	protected.HandleFunc("/data/search", browseHandler.Search).Methods("GET")
	protected.HandleFunc("/data/artists", browseHandler.Artists).Methods("GET")
	protected.HandleFunc("/data/artists/{artistId}", browseHandler.ArtistDetail).Methods("GET")
	protected.HandleFunc("/data/albums", browseHandler.Albums).Methods("GET")
	protected.HandleFunc("/data/albums/{albumId}", browseHandler.AlbumDetail).Methods("GET")

	lyricsStore := lyrics.NewStore(cfg.Data.LyricsDir)
	lyricsHandler := rest.NewLyricsHandler(db, lyricsStore)
	protected.HandleFunc("/data/tracks/lyrics", lyricsHandler.GetLyrics).Methods("GET")
	protected.HandleFunc("/data/tracks/lyrics", lyricsHandler.UpdateLyrics).Methods("POST")

	streamHandler := rest.NewStreamHandler(db, sessionStore)
	api.HandleFunc("/s/{session}/{id}", streamHandler.ServeStream).Methods("GET")
	api.HandleFunc("/s/{session}/{id}/transcode-status", streamHandler.ServeTranscodeStatus).Methods("GET")

	coverHandler := rest.NewCoverHandler(db, cfg.Data.ImagesDir, sessionStore, covers)
	api.HandleFunc("/c/{session}/{imageId}", coverHandler.Serve).Methods("GET")

	userData := rest.NewUserDataHandler(db)
	protected.HandleFunc("/user/favorites/list", userData.ListFavorites).Methods("GET")
	protected.HandleFunc("/user/favorites/add", userData.AddFavorites).Methods("POST")
	protected.HandleFunc("/user/favorites/check", userData.CheckFavorites).Methods("POST")
	protected.HandleFunc("/user/favorites/remove", userData.RemoveFavorites).Methods("POST")
	protected.HandleFunc("/user/history/list", userData.ListHistory).Methods("GET")
	protected.HandleFunc("/user/history/add", userData.AddHistory).Methods("POST")
	protected.HandleFunc("/user/history/remove", userData.RemoveHistoryItems).Methods("POST")
	protected.HandleFunc("/user/playlists", userData.ListPlaylists).Methods("GET")
	protected.HandleFunc("/user/playlists", userData.CreatePlaylist).Methods("POST")
	protected.HandleFunc("/user/playlists/{id}", userData.GetPlaylist).Methods("GET")
	protected.HandleFunc("/user/playlists/{id}", userData.DeletePlaylist).Methods("DELETE")
	protected.HandleFunc("/user/playlists/{id}/tracks/add", userData.AddTracksToPlaylist).Methods("POST")
	protected.HandleFunc("/user/playlists/{id}/tracks/remove", userData.RemoveTracksFromPlaylist).Methods("POST")
	protected.HandleFunc("/user/settings", userData.GetSettings).Methods("GET")
	protected.HandleFunc("/user/settings", userData.UpdateSettings).Methods("PUT")
	protected.HandleFunc("/user/queue", userData.GetQueue).Methods("GET")
	protected.HandleFunc("/user/queue", userData.SaveQueue).Methods("PUT")

	downloadHandler := rest.NewDownloadHandler(db, downloadManager)
	protected.HandleFunc("/libraries/{id}/downloads", downloadHandler.Create).Methods("POST")
	protected.HandleFunc("/libraries/{id}/downloads", downloadHandler.List).Methods("GET")
	protected.HandleFunc("/libraries/{id}/downloads/{jobId}", downloadHandler.Get).Methods("GET")
	protected.HandleFunc("/libraries/{id}/downloads/{jobId}", downloadHandler.Cancel).Methods("DELETE")

	// External music platforms (charts, search, details). /plat/list is
	// always registered so the frontend can distinguish "no platform
	// enabled" from a broken proxy.
	platformHandler := rest.NewPlatformHandler(platformProviders)
	protected.HandleFunc("/plat/list", platformHandler.List).Methods("GET")
	if len(platformProviders) > 0 {
		protected.HandleFunc("/plat/{name}/charts", platformHandler.ListCharts).Methods("GET")
		protected.HandleFunc("/plat/{name}/charts/{id}", platformHandler.GetChart).Methods("GET")
		protected.HandleFunc("/plat/{name}/search", platformHandler.Search).Methods("GET")
		protected.HandleFunc("/plat/{name}/tracks/{id}", platformHandler.GetTrack).Methods("GET")
		protected.HandleFunc("/plat/{name}/artists/{id}", platformHandler.GetArtist).Methods("GET")
		protected.HandleFunc("/plat/{name}/artists/{id}/tracks", platformHandler.GetArtistTracks).Methods("GET")
	}

	// Jukebox
	jukeboxHandler := rest.NewJukeboxHandler(db, engineManager, wsHub)
	jukebox := r.PathPrefix("/api/jukeboxes").Subrouter()
	jukebox.Use(middleware.AuthMiddleware(jwtService))
	jukebox.HandleFunc("", jukeboxHandler.List).Methods("GET")
	jukebox.HandleFunc("", jukeboxHandler.Create).Methods("POST")
	jukebox.HandleFunc("/{id}", jukeboxHandler.Get).Methods("GET")
	jukebox.HandleFunc("/{id}", jukeboxHandler.Update).Methods("PUT")
	jukebox.HandleFunc("/{id}", jukeboxHandler.Delete).Methods("DELETE")
	jukebox.HandleFunc("/{id}/status", jukeboxHandler.Status).Methods("GET")
	jukebox.HandleFunc("/{id}/play/{trackId}", jukeboxHandler.Play).Methods("POST")
	jukebox.HandleFunc("/{id}/stop", jukeboxHandler.Stop).Methods("POST")
	jukebox.HandleFunc("/{id}/prev", jukeboxHandler.Prev).Methods("POST")
	jukebox.HandleFunc("/{id}/next", jukeboxHandler.Next).Methods("POST")
	jukebox.HandleFunc("/{id}/volume", jukeboxHandler.Volume).Methods("PUT")
	jukebox.HandleFunc("/{id}/mode", jukeboxHandler.PlayMode).Methods("PUT")
	jukebox.HandleFunc("/{id}/queue", jukeboxHandler.Queue).Methods("GET", "POST", "DELETE")
	jukebox.HandleFunc("/{id}/queue/{index}", jukeboxHandler.RemoveFromQueue).Methods("DELETE")
	jukebox.HandleFunc("/{id}/shuffle", jukeboxHandler.Shuffle).Methods("POST")
	jukebox.HandleFunc("/{id}/queue/set", jukeboxHandler.SetQueue).Methods("PUT")
	jukebox.HandleFunc("/{id}/settings", jukeboxHandler.UpdateSettings).Methods("PUT")

	protected.HandleFunc("/audio/devices", jukeboxHandler.AudioDevices).Methods("GET")
	audioCfg := api.PathPrefix("/audio/device/configs").Subrouter()
	audioCfg.Use(middleware.AuthMiddleware(jwtService))
	audioCfg.HandleFunc("", jukeboxHandler.ListDeviceConfigs).Methods("GET")
	audioCfg.HandleFunc("", jukeboxHandler.CreateDeviceConfig).Methods("POST")
	audioCfg.HandleFunc("/available", jukeboxHandler.ListAvailableDeviceConfigs).Methods("GET")
	audioCfg.HandleFunc("/{id}", jukeboxHandler.UpdateDeviceConfig).Methods("PUT")
	audioCfg.HandleFunc("/{id}", jukeboxHandler.DeleteDeviceConfig).Methods("DELETE")

	// WebSocket
	r.HandleFunc("/ws/{session}", func(w http.ResponseWriter, r *http.Request) {
		sessionToken := mux.Vars(r)["session"]
		if sessionToken == "" {
			http.Error(w, `{"error":"missing session"}`, http.StatusUnauthorized)
			return
		}
		if _, err := sessionStore.Validate(r.Context(), sessionToken); err != nil {
			http.Error(w, `{"error":"invalid session"}`, http.StatusUnauthorized)
			return
		}
		wsHub.Handle(w, r)
	})

	// Admin
	adminHandler := rest.NewAdminHandler(db, enc)
	admin := r.PathPrefix("/api/admin").Subrouter()
	admin.Use(middleware.AuthMiddleware(jwtService))
	admin.Use(rest.AdminOnly)
	admin.HandleFunc("/users", adminHandler.ListUsers).Methods("GET")
	admin.HandleFunc("/users/{id}/role", adminHandler.UpdateUserRole).Methods("PUT")
	admin.HandleFunc("/settings", adminHandler.GetSettings).Methods("GET")
	admin.HandleFunc("/settings", adminHandler.UpdateSettings).Methods("PUT")
	admin.HandleFunc("/dirs", adminHandler.ListDirs).Methods("GET")

	// Frontend static files (SPA)
	distDir := cfg.Server.WebDir
	if distDir == "" {
		distDir = "web/dist"
	}
	fs := http.FileServer(http.Dir(distDir))
	r.PathPrefix("/assets/").Handler(http.StripPrefix("/", fs))
	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, distDir+"/index.html")
	})
}

// buildPlatformProviders instantiates the enabled external music platforms.
// The NetEase provider is always built: it powers both the platform
// endpoints (when the platform is enabled) and the metadata recognition
// chain (when the metadata source is enabled), and both share one client so
// the cookie/anon session is common.
//
// The "netease platform enabled" state is the platform switch OR the
// metadata switch: enabling NetEase metadata implies the platform is
// available too. The returned provider is never nil.
func buildPlatformProviders(cfg *config.Config) (map[string]port.PlatformProvider, *netease.Provider) {
	providers := map[string]port.PlatformProvider{}

	platformEnabled := false
	for _, name := range cfg.Platforms.Enabled {
		if name == "netease" {
			platformEnabled = true
		}
	}
	if cfg.Platforms.Netease.Enabled {
		platformEnabled = true
	}

	client := netease.NewClient()
	// Pace outbound requests so sustained scans do not trip NetEase's
	// server-side throttle (code 405 操作频繁); 0 disables pacing.
	client.SetRateLimit(cfg.Platforms.Netease.RateLimit)
	if cfg.Platforms.Netease.Cookie != "" {
		client.SetCookie(cfg.Platforms.Netease.Cookie)
	}
	neteaseProvider := netease.NewProvider(client)

	if platformEnabled || cfg.Metadata.NeteaseEnabled {
		providers["netease"] = neteaseProvider
	}
	if platformEnabled {
		log.Println("[platform] enabled: netease")
	}

	return providers, neteaseProvider
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[%s] %s %s %v", r.Method, r.URL.Path, middleware.ClientIP(r), time.Since(start))
	})
}

func (s *Server) Start() error {
	go func() {
		log.Printf("[server] listening on %s", s.http.Addr)
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[server] listen error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Print("[server] shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.vk.Close()
	err := s.http.Shutdown(ctx)
	logger.Close()
	return err
}

// settingsCacheTTL bounds how stale a cached server setting may be. Runtime
// admin changes apply within this window while hot paths (every NetEase API
// call, every cover registry build) avoid a synchronous DB read.
const settingsCacheTTL = 5 * time.Second

type settingsCacheEntry struct {
	value string
	at    time.Time
}

// cachedSettings is a per-key TTL cache for server_settings reads.
type cachedSettings struct {
	mu   sync.RWMutex
	vals map[string]settingsCacheEntry
	// refreshing marks keys with an in-flight DB refresh so concurrent
	// readers of the same stale key share one query and serve the cached
	// value meanwhile.
	refreshing map[string]bool
}

func newCachedSettings() *cachedSettings {
	return &cachedSettings{vals: make(map[string]settingsCacheEntry), refreshing: make(map[string]bool)}
}

// get returns the setting value for a key, refreshing from the repo at most
// once per TTL window. The DB query runs outside the global lock so a slow
// or failing database cannot serialize every key's read; concurrent readers
// of the same stale key share the refresh and are served the cached value.
func (c *cachedSettings) get(repo *repository.SettingsRepo, key string) string {
	c.mu.RLock()
	e, ok := c.vals[key]
	c.mu.RUnlock()
	if ok && time.Since(e.at) < settingsCacheTTL {
		return e.value
	}

	c.mu.Lock()
	if e, ok := c.vals[key]; ok && time.Since(e.at) < settingsCacheTTL {
		c.mu.Unlock()
		return e.value // another goroutine refreshed while we waited
	}
	if c.refreshing[key] {
		stale := c.vals[key].value
		c.mu.Unlock()
		return stale // a refresh is in flight; serve the cached value
	}
	c.refreshing[key] = true
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	value, err := repo.Get(ctx, key)

	c.mu.Lock()
	delete(c.refreshing, key)
	if err != nil {
		if old, ok := c.vals[key]; ok {
			// Treat the TTL as a failure backoff: refresh entry.at so a
			// flaky DB does not force every hot-path read into the (up to
			// 2s) synchronous query path on each call.
			c.vals[key] = settingsCacheEntry{value: old.value, at: time.Now()}
			c.mu.Unlock()
			log.Printf("[server] settings %q read failed: %v (using cached value)", key, err)
			return old.value
		}
		// Back off on the empty value too (cold start / never-set keys): an
		// empty string is a safe "no override, use defaults" semantic, and
		// caching it prevents every hot-path read from paying the 2s query
		// during a DB outage.
		c.vals[key] = settingsCacheEntry{value: "", at: time.Now()}
		c.mu.Unlock()
		log.Printf("[server] settings %q read failed: %v", key, err)
		return ""
	}
	c.vals[key] = settingsCacheEntry{value: value, at: time.Now()}
	c.mu.Unlock()
	return value
}

// getMany returns the values for the given keys, refreshing every stale key
// in ONE database query with a single timeout (instead of one query per key).
// The registry build reads four keys, so a cold cache backed by a failing DB
// would otherwise serialize up to four 2s timeouts (8s worst case) on a hot
// path. Absent keys are absent from the returned map ("no override").
func (c *cachedSettings) getMany(repo *repository.SettingsRepo, keys ...string) map[string]string {
	out := make(map[string]string, len(keys))
	c.mu.RLock()
	var stale []string
	for _, k := range keys {
		if e, ok := c.vals[k]; ok && time.Since(e.at) < settingsCacheTTL {
			out[k] = e.value
		} else {
			stale = append(stale, k)
		}
	}
	c.mu.RUnlock()
	if len(stale) == 0 {
		return out
	}

	c.mu.Lock()
	var toQuery []string
	for _, k := range stale {
		if e, ok := c.vals[k]; ok && time.Since(e.at) < settingsCacheTTL {
			out[k] = e.value // another goroutine refreshed it while we waited
			continue
		}
		if c.refreshing[k] {
			out[k] = c.vals[k].value // a refresh is in flight; serve the cached value
			continue
		}
		c.refreshing[k] = true
		toQuery = append(toQuery, k)
	}
	c.mu.Unlock()
	if len(toQuery) == 0 {
		return out
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	values, err := repo.GetMany(ctx, toQuery)

	c.mu.Lock()
	for _, k := range toQuery {
		delete(c.refreshing, k)
		if err == nil {
			if v, ok := values[k]; ok {
				out[k] = v
				c.vals[k] = settingsCacheEntry{value: v, at: time.Now()}
			} else {
				// key absent from DB — leave out of map per "no override" contract
				c.vals[k] = settingsCacheEntry{value: "", at: time.Now()}
			}
			continue
		}
		// On failure reuse the cached value (or empty) and back off so a flaky
		// DB does not force every hot-path read into the query path.
		old := c.vals[k]
		out[k] = old.value
		c.vals[k] = settingsCacheEntry{value: old.value, at: time.Now()}
	}
	c.mu.Unlock()
	if err != nil {
		log.Printf("[server] settings batch read failed for %d key(s): %v", len(toQuery), err)
	}
	return out
}
