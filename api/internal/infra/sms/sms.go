package sms

import (
	"context"
	"log/slog"
)

type Sender interface {
	Send(ctx context.Context, phone, code, scene string) error
}

type Mock struct {
	LastPhone string
	LastCode  string
}

func (m *Mock) Send(_ context.Context, phone, code, scene string) error {
	m.LastPhone = phone
	m.LastCode = code
	slog.Info("mock sms", "phone", phone, "scene", scene)
	return nil
}
