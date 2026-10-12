package database

import (
	"strings"
	"testing"
)

// 回归测试：带行首注释的语句不能被整段丢掉。
//
// 老实现是「以 -- 开头的片段直接 continue」，于是「注释 + ALTER」这种片段连语句
// 一起消失，但版本号仍记为已应用——列没建出来、迁移记录却显示成功。这里锁住修复：
// 开头注释要被剥掉，语句必须留下。
func TestSplitSQLKeepsStatementAfterLeadingComments(t *testing.T) {
	body := `-- 一张说明用的注释
-- 再来一行
ALTER TABLE auth_api_keys ADD COLUMN IF NOT EXISTS key_suffix TEXT NOT NULL DEFAULT '';`

	stmts := splitSQL(body)
	if len(stmts) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(stmts), stmts)
	}
	if !strings.HasPrefix(stmts[0], "ALTER TABLE") {
		t.Fatalf("statement not preserved: %q", stmts[0])
	}
}

// 语句内部的注释要保留（PostgreSQL 能解析，迁移文件可把说明写在语句里）。
func TestSplitSQLKeepsInlineComments(t *testing.T) {
	body := `CREATE TABLE t (
    -- 主键
    id TEXT PRIMARY KEY
);`
	stmts := splitSQL(body)
	if len(stmts) != 1 {
		t.Fatalf("want 1 statement, got %d: %#v", len(stmts), stmts)
	}
	if !strings.Contains(stmts[0], "主键") {
		t.Fatalf("inline comment dropped: %q", stmts[0])
	}
}

// 空片与纯注释片不产生语句。
func TestSplitSQLDropsEmptyAndCommentOnlyChunks(t *testing.T) {
	body := "-- 只解释一下\n;\n-- 另一个纯注释\n"
	if stmts := splitSQL(body); len(stmts) != 0 {
		t.Fatalf("want 0 statements, got %#v", stmts)
	}
}

// 同一文件多条语句照常拆分。
func TestSplitSQLMultipleStatements(t *testing.T) {
	body := "SELECT 1;\nSELECT 2;\n"
	if stmts := splitSQL(body); len(stmts) != 2 {
		t.Fatalf("want 2 statements, got %#v", stmts)
	}
}