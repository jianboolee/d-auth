package mailer

import (
	"context"
	"log/slog"
)

type Sender interface {
	SendCode(ctx context.Context, email, code, scene string) error
}

type Mock struct {
	LastEmail string
	LastCode  string
}

func (m *Mock) SendCode(_ context.Context, email, code, scene string) error {
	m.LastEmail = email
	m.LastCode = code
	slog.Info("mock mail", "email", email, "code", code, "scene", scene)
	return nil
}
