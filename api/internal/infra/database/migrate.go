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
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS auth_schema_migrations (
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
		if err := db.Raw("SELECT COUNT(1) FROM auth_schema_migrations WHERE version = ?", name).Scan(&n).Error; err != nil {
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
		if err := db.Exec("INSERT INTO auth_schema_migrations (version) VALUES (?)", name).Error; err != nil {
			return err
		}
	}
	return nil
}

// splitSQL 把迁移文件按分号切成一条条语句。
//
// 注意不能「整段丢弃以 -- 开头的片段」：一条「整行注释 + 真正的语句」的片段会被
// 连语句一起丢掉，而版本号照样记为已应用——列没建出来、库却在迁移记录里显示成功，
// 这种静默失败会在下次 INSERT/查询时才炸出来。所以这里只剥掉片段开头的注释行，
// 剩下还有内容的片段一律保留。
func splitSQL(body string) []string {
	raw := strings.Split(body, ";")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if stmt := stripLeadingComments(s); stmt != "" {
			out = append(out, stmt)
		}
	}
	return out
}

// stripLeadingComments 去掉片段开头连续的整行注释与空行，返回 TrimSpace 后的语句。
// 只剥开头的注释，语句内部的行内注释保留（PostgreSQL 自己能解析，也让迁移文件
// 可以像现在这样把说明写在语句里）。
func stripLeadingComments(chunk string) string {
	lines := strings.Split(chunk, "\n")
	i := 0
	for i < len(lines) {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "--") {
			i++
			continue
		}
		break
	}
	return strings.TrimSpace(strings.Join(lines[i:], "\n"))
}
