package httpx

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/contextx"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/modules/auth"
	"github.com/jianboolee/d-auth/internal/modules/store"
	"github.com/jianboolee/d-auth/internal/response"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-Id")
		if id == "" {
			id = idgen.NewString()
		}
		c.Header("X-Request-Id", id)
		c.Next()
	}
}

func Recover() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, rec any) {
		slog.Error("panic", "err", rec)
		response.Fail(c, http.StatusInternalServerError, "INTERNAL", "服务器错误")
	})
}

func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("http",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
		)
	}
}

func CORS(cfg *config.Config) gin.HandlerFunc {
	allow := map[string]struct{}{}
	for _, o := range cfg.AllowedOrigins {
		allow[o] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if _, ok := allow[origin]; ok || cfg.IsDev() {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
				c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-DAuth-Client, X-Request-Id")
				c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				c.Header("Vary", "Origin")
			}
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func Authenticate(cfg *config.Config, authSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if strings.HasPrefix(header, "Bearer ") {
			raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			if strings.HasPrefix(raw, "dak_live_") {
				if !loadAPIKey(c, authSvc, raw) {
					return
				}
				c.Next()
				return
			}
			claims, err := authSvc.ParseAccess(raw)
			if err != nil {
				response.Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "登录已失效")
				return
			}
			uid, _ := uuid.Parse(claims.Subject)
			sid, _ := uuid.Parse(claims.SessionID)
			cid, _ := uuid.Parse(claims.ClientID)
			user, err := authSvc.Store().UserByID(uid)
			if err != nil || user.Status != "active" {
				response.Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "登录已失效")
				return
			}
			roles := claims.Roles
			if len(roles) == 0 {
				roles, _ = authSvc.Store().RoleCodes(uid)
			}
			contextx.SetActor(c, contextx.Actor{
				Kind: contextx.ActorUser, UserID: uid, SessionID: sid, ClientID: cid, Roles: roles,
			})
			c.Next()
			return
		}

		sid, _ := c.Cookie(cfg.CookieName)
		if sid == "" {
			response.Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
			return
		}
		id, err := uuid.Parse(sid)
		if err != nil {
			response.Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions {
			if c.GetHeader("X-DAuth-Client") == "" {
				response.Fail(c, http.StatusForbidden, "CSRF", "缺少 X-DAuth-Client")
				return
			}
		}
		sess, err := authSvc.SessionByID(id)
		if err != nil {
			response.Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "登录已失效")
			return
		}
		user, err := authSvc.Store().UserByID(sess.UserID)
		if err != nil || user.Status != "active" {
			response.Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "登录已失效")
			return
		}
		roles, _ := authSvc.Store().RoleCodes(user.ID)
		cid := uuid.Nil
		if sess.ClientID != nil {
			cid = *sess.ClientID
		}
		contextx.SetActor(c, contextx.Actor{
			Kind: contextx.ActorUser, UserID: user.ID, SessionID: sess.ID, ClientID: cid, Roles: roles,
		})
		c.Next()
	}
}

func loadAPIKey(c *gin.Context, authSvc *auth.Service, raw string) bool {
	prefix := store.PrefixOfAPIKey(raw)
	key, err := authSvc.Store().FindAPIKeyByPrefix(prefix)
	if err != nil || key.KeyHash != store.HashToken(raw) {
		response.Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "无效的 API Key")
		return false
	}
	if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
		response.Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "API Key 已过期")
		return false
	}
	now := time.Now()
	_ = authSvc.Store().DB.Model(key).Update("last_used_at", now).Error
	contextx.SetActor(c, contextx.Actor{
		Kind: contextx.ActorAPIKey, APIKeyID: key.ID, Scopes: key.Scopes,
	})
	return true
}

func RequireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		a, ok := contextx.GetActor(c)
		if !ok || a.Kind != contextx.ActorUser {
			response.Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
			return
		}
		c.Next()
	}
}

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		a, ok := contextx.GetActor(c)
		if !ok {
			response.Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
			return
		}
		if a.Kind == contextx.ActorUser && (contextx.HasRole(a.Roles, "admin") || contextx.HasRole(a.Roles, "superadmin")) {
			c.Next()
			return
		}
		if a.Kind == contextx.ActorAPIKey && (contextx.HasScope(a.Scopes, "users:read") || contextx.HasScope(a.Scopes, "users:write") || contextx.HasScope(a.Scopes, "*")) {
			c.Next()
			return
		}
		response.Fail(c, http.StatusForbidden, "FORBIDDEN", "权限不足")
	}
}

func RequireScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		a, ok := contextx.GetActor(c)
		if !ok {
			response.Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
			return
		}
		if a.Kind == contextx.ActorUser && (contextx.HasRole(a.Roles, "admin") || contextx.HasRole(a.Roles, "superadmin")) {
			c.Next()
			return
		}
		if a.Kind == contextx.ActorAPIKey && contextx.HasScope(a.Scopes, scope) {
			c.Next()
			return
		}
		response.Fail(c, http.StatusForbidden, "FORBIDDEN", "权限不足")
	}
}

func RequireSuperAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		a, ok := contextx.GetActor(c)
		if !ok || a.Kind != contextx.ActorUser || !contextx.HasRole(a.Roles, "superadmin") {
			response.Fail(c, http.StatusForbidden, "FORBIDDEN", "需要超级管理员")
			return
		}
		c.Next()
	}
}

func WriteError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	if code, msg, ok := apperr.CodeOf(err); ok {
		status := http.StatusBadRequest
		switch {
		case strings.HasPrefix(code, "UNAUTH") || code == "INVALID_CREDENTIALS" || code == "INVALID_CODE" || code == "CODE_LOCKED":
			status = http.StatusUnauthorized
			if code == "INVALID_CODE" || code == "CODE_LOCKED" {
				status = http.StatusBadRequest
			}
		case code == "FORBIDDEN" || code == "CSRF":
			status = http.StatusForbidden
		case code == "NOT_FOUND" || code == "QR_EXPIRED":
			status = http.StatusNotFound
		case code == "PHONE_TAKEN" || code == "EMAIL_TAKEN" || code == "CLIENT_TAKEN":
			status = http.StatusConflict
		case code == "SEND_TOO_FAST" || code == "SEND_LIMIT":
			status = http.StatusTooManyRequests
		case code == "DISABLED":
			status = http.StatusForbidden
		}
		response.Fail(c, status, code, msg)
		return
	}
	slog.Error("handler", "err", err)
	response.Fail(c, http.StatusInternalServerError, "INTERNAL", "服务器错误")
}
