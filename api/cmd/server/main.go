package main

import (
	"log/slog"
	"os"

	"github.com/jianboolee/d-auth/internal/app"
	"github.com/jianboolee/d-auth/internal/config"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	cfg := config.Load()
	engine, err := app.New(cfg)
	if err != nil {
		slog.Error("boot", "err", err)
		os.Exit(1)
	}
	slog.Info("listening", "addr", cfg.HTTPAddr)
	if err := engine.Run(cfg.HTTPAddr); err != nil {
		slog.Error("run", "err", err)
		os.Exit(1)
	}
}
