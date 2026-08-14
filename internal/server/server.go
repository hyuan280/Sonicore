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
	"github.com/sonicore/server/internal/infrastructure/lyrics"
	"github.com/sonicore/server/internal/infrastructure/metadata"
	"github.com/sonicore/server/internal/infrastructure/player"
	"github.com/sonicore/server/internal/infrastructure/repository"
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
	// One cover manager shared by the scanner and the HTTP cover handlers so
	// extraction is serialized across both paths.
	covers := metadata.NewCoverManager(cfg.Data.ImagesDir, db)
	scannerService := service.NewScannerService(db, cfg.Data.ImagesDir, cfg.Data.LyricsDir, mbCfg, covers)
	downloadManager := download.NewManager(db)
	wsHub := ws.NewHub()

	platformProviders := buildPlatformProviders(cfg)

	router := mux.NewRouter()
	middleware.SetTrustedProxies(cfg.Server.TrustedProxies)
	registerRoutes(router, db, jwtService, tokenStore, sessionStore, scannerService, downloadManager, engineManager, wsHub, refreshExp, cfg, platformProviders, covers)

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

func registerRoutes(r *mux.Router, db *sql.DB, jwtService *auth.JWTService, tokenStore *cache.TokenStore, sessionStore *cache.SessionStore, scannerService *service.ScannerService, downloadManager *download.Manager, engineManager *player.EngineManager, wsHub *ws.Hub, refreshExp time.Duration, cfg *config.Config, platformProviders map[string]port.PlatformProvider, covers *metadata.CoverManager) {
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
	})
	protected.HandleFunc("/metadata/identify", metadataHandler.Identify).Methods("POST")
	protected.HandleFunc("/metadata/reidentify", metadataHandler.Reidentify).Methods("POST")
	protected.HandleFunc("/metadata/search/track", metadataHandler.SearchTrack).Methods("POST")
	protected.HandleFunc("/metadata/save", metadataHandler.Save).Methods("POST")
	protected.HandleFunc("/metadata/search/artist", metadataHandler.SearchArtist).Methods("POST")
	protected.HandleFunc("/metadata/search/album", metadataHandler.SearchRelease).Methods("POST")

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
	adminHandler := rest.NewAdminHandler(db)
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
func buildPlatformProviders(cfg *config.Config) map[string]port.PlatformProvider {
	providers := map[string]port.PlatformProvider{}

	enabled := map[string]bool{}
	for _, name := range cfg.Platforms.Enabled {
		enabled[name] = true
	}
	if cfg.Platforms.Netease.Enabled {
		enabled["netease"] = true
	}

	if enabled["netease"] {
		client := netease.NewClient()
		if cfg.Platforms.Netease.Cookie != "" {
			client.SetCookie(cfg.Platforms.Netease.Cookie)
		}
		providers["netease"] = netease.NewProvider(client)
		log.Println("[platform] enabled: netease")
	}

	return providers
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
	return s.http.Shutdown(ctx)
}
