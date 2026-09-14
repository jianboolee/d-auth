package httpx

import (
	"github.com/gin-gonic/gin"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/infra/ossstore"
	"github.com/jianboolee/d-auth/internal/modules/admin"
	"github.com/jianboolee/d-auth/internal/modules/auth"
	"github.com/jianboolee/d-auth/internal/modules/usersvc"
)

func NewRouter(cfg *config.Config, authSvc *auth.Service, users *usersvc.Service, adm *admin.Service) *gin.Engine {
	if cfg.IsDev() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(Recover(), RequestID(), Logger(), CORS(cfg))

	h := &Handlers{Cfg: cfg, Auth: authSvc, Users: users, Admin: adm}

	r.GET("/health", h.Health)
	r.GET("/static/*path", StaticUploads(ossstore.DataRoot()))

	v1 := r.Group("/v1")
	{
		v1.GET("/.well-known/jwks.json", h.JWKS)
		authg := v1.Group("/auth")
		{
			authg.POST("/sms/send", h.SendSMS)
			authg.POST("/sms/login", h.SMSLogin)
			authg.POST("/email/send-code", h.SendEmail)
			authg.POST("/email/register", h.EmailRegister)
			authg.POST("/email/login", h.EmailLogin)
			authg.POST("/password/forgot", h.ForgotPassword)
			authg.POST("/password/reset", h.ResetPassword)
			authg.POST("/refresh", h.Refresh)
			authg.POST("/logout", h.Logout)
			authg.POST("/wechat/qr/start", h.WeChatQRStart)
			authg.GET("/wechat/qr/status", h.WeChatQRStatus)
			authg.GET("/wechat/callback", h.WeChatCallback)
			authg.POST("/wechat/mini", h.WeChatMini)
			authg.GET("/wechat/mock-scan", h.WeChatMockScan)
			authg.POST("/wechat/mock-scan", h.WeChatMockScan)
		}

		me := v1.Group("")
		me.Use(Authenticate(cfg, authSvc), RequireUser())
		{
			me.GET("/me", h.Me)
			me.PATCH("/me", h.PatchMe)
			me.POST("/me/password", h.ChangePassword)
			me.POST("/me/phone/change", h.ChangePhone)
			me.POST("/me/email/bind", h.BindEmail)
			me.GET("/me/identities", h.Identities)
			me.DELETE("/me/identities/:id", h.DeleteIdentity)
			me.GET("/me/sessions", h.Sessions)
			me.DELETE("/me/sessions/:id", h.DeleteSession)
			me.POST("/me/avatar/sign", h.SignAvatar)
			me.POST("/me/avatar/confirm", h.ConfirmAvatar)
			me.PUT("/me/avatar/upload", h.UploadAvatar)
		}

		adminG := v1.Group("/admin")
		adminG.Use(Authenticate(cfg, authSvc), RequireAdmin())
		{
			adminG.GET("/users", RequireScope("users:read"), h.AdminUsers)
			adminG.GET("/users/:id", RequireScope("users:read"), h.AdminUser)
			adminG.POST("/users", RequireScope("users:write"), h.AdminCreateUser)
			adminG.PATCH("/users/:id", RequireScope("users:write"), h.AdminPatchUser)
			adminG.POST("/users/:id/password", RequireScope("users:write"), h.AdminResetPassword)
			adminG.GET("/roles", RequireScope("users:read"), h.AdminRoles)
			adminG.GET("/clients", RequireScope("users:write"), h.AdminClients)
			adminG.POST("/clients", RequireSuperAdmin(), h.AdminCreateClient)
			adminG.PATCH("/clients/:id", RequireSuperAdmin(), h.AdminPatchClient)
			adminG.GET("/api-keys", RequireSuperAdmin(), h.AdminAPIKeys)
			adminG.POST("/api-keys", RequireSuperAdmin(), h.AdminCreateAPIKey)
			adminG.PATCH("/api-keys/:id", RequireSuperAdmin(), h.AdminPatchAPIKey)
			adminG.GET("/audit-logs", RequireScope("users:read"), h.AdminAudit)
		}

		internal := v1.Group("/internal")
		internal.Use(Authenticate(cfg, authSvc), RequireScope("users:read"))
		{
			internal.GET("/users/:id", h.InternalUser)
			internal.GET("/users", h.InternalLookup)
		}
	}
	return r
}
