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
	"github.com/jianboolee/d-auth/internal/models"
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

// resolveActor 解析请求的登录态，成功返回 actor 与当前会话（会话在 Bearer
// 路径下可能拿不到，此时为 nil）。它不写任何响应，失败时返回带码的错误，
// 由调用方决定如何响应：Authenticate 交给 WriteError 返回原状态码，
// Session 探针则一律当作未登录返回空对象。
func resolveActor(c *gin.Context, cfg *config.Config, authSvc *auth.Service) (*contextx.Actor, *models.Session, error) {
	errUnauthed := apperr.New(apperr.ErrUnauthorized, "UNAUTHORIZED", "登录已失效")
	errNotLoggedIn := apperr.New(apperr.ErrUnauthorized, "UNAUTHORIZED", "未登录")

	header := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(header, "Bearer ") {
		raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if strings.HasPrefix(raw, "dak_live_") {
			prefix := store.PrefixOfAPIKey(raw)
			key, err := authSvc.Store().FindAPIKeyByPrefix(prefix)
			if err != nil || key.KeyHash != store.HashToken(raw) {
				return nil, nil, apperr.New(apperr.ErrUnauthorized, "UNAUTHORIZED", "无效的 API Key")
			}
			if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
				return nil, nil, apperr.New(apperr.ErrUnauthorized, "UNAUTHORIZED", "API Key 已过期")
			}
			now := time.Now()
			_ = authSvc.Store().DB.Model(key).Update("last_used_at", now).Error
			return &contextx.Actor{
				Kind: contextx.ActorAPIKey, APIKeyID: key.ID, Scopes: key.Scopes,
			}, nil, nil
		}
		claims, err := authSvc.ParseAccess(raw)
		if err != nil {
			return nil, nil, errUnauthed
		}
		uid, err := idgen.ParseUserID(claims.Subject)
		if err != nil {
			return nil, nil, errUnauthed
		}
		sid, _ := uuid.Parse(claims.SessionID)
		cid, _ := uuid.Parse(claims.ClientID)
		user, err := authSvc.Store().UserByID(uid)
		if err != nil || user.Status != "active" {
			return nil, nil, errUnauthed
		}
		roles := claims.Roles
		if len(roles) == 0 {
			roles, _ = authSvc.Store().RoleCodes(uid)
		}
		var sess *models.Session
		if sid != uuid.Nil {
			if s, err := authSvc.SessionByID(sid); err == nil {
				sess = s
			}
		}
		return &contextx.Actor{
			Kind: contextx.ActorUser, UserID: uid, SessionID: sid, ClientID: cid, Roles: roles,
		}, sess, nil
	}

	sid, _ := c.Cookie(cfg.CookieName)
	if sid == "" {
		return nil, nil, errNotLoggedIn
	}
	id, err := uuid.Parse(sid)
	if err != nil {
		return nil, nil, errNotLoggedIn
	}
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions {
		if c.GetHeader("X-DAuth-Client") == "" {
			return nil, nil, apperr.New(apperr.ErrForbidden, "CSRF", "缺少 X-DAuth-Client")
		}
	}
	sess, err := authSvc.SessionByID(id)
	if err != nil {
		return nil, nil, errUnauthed
	}
	user, err := authSvc.Store().UserByID(sess.UserID)
	if err != nil || user.Status != "active" {
		return nil, nil, errUnauthed
	}
	roles, _ := authSvc.Store().RoleCodes(user.ID)
	cid := uuid.Nil
	if sess.ClientID != nil {
		cid = *sess.ClientID
	}
	return &contextx.Actor{
		Kind: contextx.ActorUser, UserID: user.ID, SessionID: sess.ID, ClientID: cid, Roles: roles,
	}, sess, nil
}

func Authenticate(cfg *config.Config, authSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, _, err := resolveActor(c, cfg, authSvc)
		if err != nil {
			WriteError(c, err)
			return
		}
		contextx.SetActor(c, *actor)
		c.Next()
	}
}

// OptionalAuth 解析登录态但允许匿名通过，用于 /refresh、/logout 这类公开路由。
// 它们需要知道「是谁在调用」才能吊销对应会话，但未登录时也必须能调用且返回 200
// （幂等退出）。因此只拦 CSRF：cookie 凭据下的状态变更缺少 X-DAuth-Client 就拒绝，
// 其余错误一律放行。
func OptionalAuth(cfg *config.Config, authSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, _, err := resolveActor(c, cfg, authSvc)
		if err != nil {
			if code, _, _ := apperr.CodeOf(err); code == "CSRF" {
				WriteError(c, err)
				return
			}
			// 未登录或凭据失效：不是错误，交给 handler 按匿名处理
			c.Next()
			return
		}
		contextx.SetActor(c, *actor)
		c.Next()
	}
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

// RequirePhone 拦住尚未绑定手机号的用户。手机号是主要登录能力，未绑定时
// 只放行绑定手机号本身（POST /me/phone/change），其余账号接口返回 PHONE_REQUIRED。
// 这里按需查询（只在这组路由上付出一次查询），而不是给所有已登录请求都加开销。
func RequirePhone(authSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		a, ok := contextx.GetActor(c)
		if !ok || a.Kind != contextx.ActorUser {
			response.Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", "未登录")
			return
		}
		hasPhone, err := authSvc.Store().HasIdentity(a.UserID, models.IdentityPhone)
		if err != nil {
			WriteError(c, err)
			return
		}
		if !hasPhone {
			response.Fail(c, http.StatusForbidden, "PHONE_REQUIRED", "请先绑定手机号")
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
		case code == "PHONE_TAKEN" || code == "EMAIL_TAKEN" || code == "CLIENT_TAKEN" || code == "ROLE_TAKEN":
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
