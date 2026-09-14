package database

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gorm.io/gorm"
)

func Migrate(db *gorm.DB, dir string) error {
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`).Error; err != nil {
		return err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var n int64
		if err := db.Raw("SELECT COUNT(1) FROM schema_migrations WHERE version = ?", name).Scan(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		for _, stmt := range splitSQL(string(body)) {
			if err := db.Exec(stmt).Error; err != nil {
				return fmt.Errorf("apply %s: %w", name, err)
			}
		}
		if err := db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", name).Error; err != nil {
			return err
		}
	}
	return nil
}

func splitSQL(body string) []string {
	raw := strings.Split(body, ";")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		out = append(out, s)
	}
	return out
}
