package idgen_test

import (
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/idgen"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 验证用户 ID 能原样通过 Postgres 的 uuid 列往返：写进去的字节读回来不变，
// 对外格式化的结果也与写入时一致。
// 需要本地 postgres，通过 PG_TEST_DSN 提供连接串，否则跳过。
type row struct {
	ID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	Name string
}

func (row) TableName() string { return "idgen_roundtrip" }

func TestUserIDPostgresUUIDRoundTrip(t *testing.T) {
	dsn := os.Getenv("PG_TEST_DSN")
	if dsn == "" {
		t.Skip("PG_TEST_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Exec("DROP TABLE IF EXISTS idgen_roundtrip").Error; err != nil {
		t.Fatalf("drop: %v", err)
	}
	if err := db.Exec("CREATE TABLE idgen_roundtrip (id UUID PRIMARY KEY, name TEXT)").Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	defer db.Exec("DROP TABLE IF EXISTS idgen_roundtrip")

	newIDs := make([]string, 0, 50)
	rows := make([]row, 0, 50)
	for i := 0; i < 50; i++ {
		id := idgen.NewUserID()
		s := idgen.FormatUserID(id)
		newIDs = append(newIDs, s)
		rows = append(rows, row{ID: id, Name: s})
	}

	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}

	var got []row
	if err := db.Order("name").Find(&got).Error; err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(got) != len(rows) {
		t.Fatalf("got %d rows want %d", len(got), len(rows))
	}
	seen := map[string]bool{}
	for _, r := range got {
		s := idgen.FormatUserID(r.ID)
		if s != r.Name {
			t.Fatalf("round trip broke: stored name %q, formatted id %q", r.Name, s)
		}
		seen[s] = true
	}
	for _, s := range newIDs {
		if !seen[s] {
			t.Fatalf("id %q missing after round trip", s)
		}
	}
}
