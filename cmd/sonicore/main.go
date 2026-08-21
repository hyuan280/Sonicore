package main

import (
	"log"

	"github.com/sonicore/server/internal/config"
	"github.com/sonicore/server/internal/infrastructure/logger"
	"github.com/sonicore/server/internal/server"
)

func main() {
	cfg := config.Load()

	if err := logger.Init(logger.Config{
		Level:      cfg.Log.Level,
		Format:     cfg.Log.Format,
		FileOutput: cfg.Log.FileOutput,
		FilePath:   cfg.Log.FilePath,
		DataDir:    cfg.Data.DataDir,
		MaxSize:    cfg.Log.MaxSize,
		MaxAge:     cfg.Log.MaxAge,
		MaxBackups: cfg.Log.MaxBackups,
	}); err != nil {
		log.Fatalf("failed to init logger: %v", err)
	}

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}

	if err := srv.Start(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
