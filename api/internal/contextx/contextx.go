package contextx

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type ActorKind string

const (
	ActorUser   ActorKind = "user"
	ActorAPIKey ActorKind = "api_key"
)

type Actor struct {
	Kind      ActorKind
	UserID    uuid.UUID
	APIKeyID  uuid.UUID
	SessionID uuid.UUID
	ClientID  uuid.UUID
	Roles     []string
	Scopes    []string
}

func SetActor(c *gin.Context, a Actor) {
	c.Set("dauth.actor", a)
}

func GetActor(c *gin.Context) (Actor, bool) {
	v, ok := c.Get("dauth.actor")
	if !ok {
		return Actor{}, false
	}
	a, ok := v.(Actor)
	return a, ok
}

func HasRole(roles []string, want string) bool {
	for _, r := range roles {
		if r == want || r == "superadmin" {
			return true
		}
	}
	return false
}

func HasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want || s == "*" {
			return true
		}
	}
	return false
}
