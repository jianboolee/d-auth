package httpx

import (
	"crypto/hmac"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/contextx"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/modules/auth"
	"github.com/jianboolee/d-auth/internal/modules/store"
	"github.com/jianboolee/d-auth/internal/response"
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

// resolveActor 解析请求的登录态。它不写任何响应，失败时返回带码的错误，
// 由调用方决定如何响应：Authenticate 交给 WriteError 返回原状态码，
// Session 探针则一律当作未登录返回空对象。
//
// 用户态的两条路径都是「验签即信」，不查库、不查 Redis：
//
//   - Bearer <access JWT>：子系统 / App 带上来的 access token。
//   - cookie <access JWT>（CookieName，默认 dauth_access）：浏览器。
//
// 角色从 JWT claims 直接读。这意味着停用 / 改密后，已签发的 access token 在
// TTL 内仍会通过校验——这是纯验签方案换取「读路径零存储访问」的既定窗口，
// 续期侧（refresh）才是真正的吊销点（见 sessionstore）。
// API Key 分支仍查库：它是服务端凭据、不是自证 JWT，且不参与浏览器会话。
func resolveActor(c *gin.Context, cfg *config.Config, authSvc *auth.Service) (*contextx.Actor, error) {
	errUnauthed := apperr.New(apperr.ErrUnauthorized, "UNAUTHORIZED", "登录已失效")
	errNotLoggedIn := apperr.New(apperr.ErrUnauthorized, "UNAUTHORIZED", "未登录")

	header := strings.TrimSpace(c.GetHeader("Authorization"))
	if strings.HasPrefix(header, "Bearer ") {
		raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if strings.HasPrefix(raw, "sk-") {
			prefix := store.PrefixOfAPIKey(raw)
			key, err := authSvc.Store().FindAPIKeyByPrefix(prefix)
			// 用 hmac.Equal 常量时间比较哈希，避免按字节短路带来的时序差异。
			if err != nil || !hmac.Equal([]byte(key.KeyHash), []byte(store.HashToken(raw))) {
				return nil, apperr.New(apperr.ErrUnauthorized, "UNAUTHORIZED", "无效的 API Key")
			}
			if key.ExpiresAt != nil && key.ExpiresAt.Before(time.Now()) {
				return nil, apperr.New(apperr.ErrUnauthorized, "UNAUTHORIZED", "API Key 已过期")
			}
			now := time.Now()
			_ = authSvc.Store().DB.Model(key).Update("last_used_at", now).Error
			return &contextx.Actor{
				Kind: contextx.ActorAPIKey, APIKeyID: key.ID, Scopes: key.Scopes,
			}, nil
		}
		return actorFromJWT(authSvc, raw, errUnauthed)
	}

	// CSRF：cookie 凭据下的状态变更请求必须带 X-DAuth-Client。cookie 会自动
	// 随跨站请求携带，SameSite=Lax 只挡住跨站 POST，加一道自定义头校验作为兜底。
	if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions {
		if c.GetHeader("X-DAuth-Client") == "" {
			return nil, apperr.New(apperr.ErrForbidden, "CSRF", "缺少 X-DAuth-Client")
		}
	}

	raw, err := c.Cookie(cfg.CookieName)
	if err != nil || raw == "" {
		return nil, errNotLoggedIn
	}
	return actorFromJWT(authSvc, raw, errUnauthed)
}

// actorFromJWT 验签一枚 access token 并组装 Actor。角色取自 claims，
// 不查库补齐——claims 缺失角色时视为该用户没有角色（而非回退查库）。
func actorFromJWT(authSvc *auth.Service, raw string, errUnauthed error) (*contextx.Actor, error) {
	claims, err := authSvc.ParseAccess(raw)
	if err != nil {
		return nil, errUnauthed
	}
	uid, err := idgen.ParseUserID(claims.Subject)
	if err != nil {
		return nil, errUnauthed
	}
	cid, _ := uuid.Parse(claims.ClientID)
	return &contextx.Actor{
		Kind: contextx.ActorUser, UserID: uid, ClientID: cid, Roles: claims.Roles,
	}, nil
}

func Authenticate(cfg *config.Config, authSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		actor, err := resolveActor(c, cfg, authSvc)
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
		actor, err := resolveActor(c, cfg, authSvc)
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
