package main

import (
	"log/slog"
	"os"

	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/infra/database"
)

func main() {
	// 兼容发布镜像 entrypoint 的 `migrate up`
	if len(os.Args) > 1 && os.Args[1] == "down" {
		slog.Error("migrate down is not implemented; restore from backup")
		os.Exit(1)
	}
	cfg := config.Load()
	db, err := database.Open(cfg.PostgresDSN)
	if err != nil {
		slog.Error("postgres", "err", err)
		os.Exit(1)
	}
	if err := database.Migrate(db, "migrations"); err != nil {
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}
	slog.Info("migrations applied")
}
