package idgen

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewIsV7(t *testing.T) {
	id := New()
	if id.Version() != 7 {
		t.Fatalf("got version %d want 7", id.Version())
	}
	if id == uuid.Nil {
		t.Fatal("nil uuid")
	}
}
