package idgen

import "github.com/google/uuid"

func New() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Must(uuid.NewV7())
	}
	return id
}

func NewString() string {
	return New().String()
}
