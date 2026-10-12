package httpx

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/infra/ossstore"
	"github.com/jianboolee/d-auth/internal/modules/admin"
	"github.com/jianboolee/d-auth/internal/modules/auth"
	"github.com/jianboolee/d-auth/internal/modules/legal"
	"github.com/jianboolee/d-auth/internal/modules/profile"
	"github.com/jianboolee/d-auth/internal/modules/usersvc"
)

func NewRouter(cfg *config.Config, authSvc *auth.Service, users *usersvc.Service, adm *admin.Service, legalSvc *legal.Service, profiles *profile.Service) *gin.Engine {
	if cfg.IsDev() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	// 只信任配置里的代理，ClientIP() 才会采信 X-Forwarded-For。
	// gin 默认信任所有代理，那样 XFF 可被伪造，按 IP 限流会形同虚设。
	// 配错（未包含真实代理）会让所有请求塌缩成同一个 IP，反而误封所有人。
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		slog.Error("set trusted proxies", "err", err, "proxies", cfg.TrustedProxies)
	}
	r.Use(Recover(), RequestID(), Logger(), CORS(cfg))

	h := &Handlers{Cfg: cfg, Auth: authSvc, Users: users, Admin: adm, LegalDocs: legalSvc, Profiles: profiles}

	r.GET("/health", h.Health)
	r.GET("/static/*path", StaticUploads(ossstore.DataRoot()))

	v1 := r.Group("/v1")
	{
		v1.GET("/.well-known/jwks.json", h.JWKS)
		authg := v1.Group("/auth")
		{
			// 登录探针：公开路由，未登录返回 200 空对象（不能挂 Authenticate，
			// 否则未登录会 401）。
			authg.GET("/session", h.Session)
			// 用户协议 / 隐私政策（公开读）：登录页在未登录时也会链到这两个页面。
			authg.GET("/legal", h.Legal)
			authg.POST("/sms/send", h.SendSMS)
			authg.POST("/sms/login", h.SMSLogin)
			authg.POST("/email/send-code", h.SendEmail)
			authg.POST("/email/register", h.EmailRegister)
			// 密码登录：identifier 可传手机号或邮箱
			authg.POST("/password/login", h.PasswordLogin)
			// 仅邮箱的旧接口，保留兼容已发布的 SDK
			authg.POST("/email/login", h.EmailLogin)
			authg.POST("/password/forgot", h.ForgotPassword)
			authg.POST("/password/reset", h.ResetPassword)
			// refresh / logout 需要知道调用者是谁才能吊销对应会话，
			// 但未登录也必须可调用（幂等退出），所以用 OptionalAuth。
			authg.POST("/refresh", OptionalAuth(cfg, authSvc), h.Refresh)
			authg.POST("/logout", OptionalAuth(cfg, authSvc), h.Logout)
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
			// 绑定/更换手机号本身不能要求已有手机号，否则新用户无法完成绑定。
			me.POST("/me/phone/change", h.ChangePhone)

			// 注销账号同样不要求已绑手机号：手机号已失效、无法绑定的人
			// 也应该能注销自己的账号（与上面的绑定接口同理）。
			me.DELETE("/me", h.DeleteAccount)
			// 注销前的短信验证码：给当前绑定手机号发送，号码由服务端解析。
			// 与注销同组（不要求已绑手机号），未绑定时由服务端返回 NO_PHONE。
			me.POST("/me/sms/self", h.SendSelfSMS)

			// 其余账号接口要求已绑定手机号：手机号是主要登录能力，未绑定时
			// 只放行上面的绑定接口，前端会引导到绑定手机号页。
			gated := me.Group("")
			gated.Use(RequirePhone(authSvc))
			{
				gated.GET("/me", h.Me)
				gated.PATCH("/me", h.PatchMe)
				gated.POST("/me/password", h.ChangePassword)
				gated.POST("/me/email/bind", h.BindEmail)
				gated.GET("/me/identities", h.Identities)
				gated.DELETE("/me/identities/:id", h.DeleteIdentity)
				gated.GET("/me/sessions", h.Sessions)
				gated.DELETE("/me/sessions/:id", h.DeleteSession)
				gated.POST("/me/avatar/sign", h.SignAvatar)
				gated.POST("/me/avatar/confirm", h.ConfirmAvatar)
				gated.PUT("/me/avatar/upload", h.UploadAvatar)
			}
		}

		adminG := v1.Group("/admin")
		adminG.Use(Authenticate(cfg, authSvc), RequireAdmin())
		{
			adminG.GET("/users", RequireScope("users:read"), h.AdminUsers)
			// 仪表盘概览：用户总数 / 活跃 / 今日与昨日注册，只读，与列表接口同权限
			adminG.GET("/stats", RequireScope("users:read"), h.AdminStats)
			adminG.GET("/users/:id", RequireScope("users:read"), h.AdminUser)
			// 用户的登录会话独立分页，不塞进详情响应（会话数可能很多）
			adminG.GET("/users/:id/sessions", RequireScope("users:read"), h.AdminUserSessions)
			adminG.POST("/users", RequireScope("users:write"), h.AdminCreateUser)
			// 生成虚拟手机号（120 开头），供建号表单「生成虚拟号」按钮填入；不写库。
			// 路径放在 /users 之外，与 /users/:id 这类参数路由保持分离。
			adminG.POST("/virtual-phone", RequireScope("users:write"), h.AdminGeneratePhone)
			adminG.PATCH("/users/:id", RequireScope("users:write"), h.AdminPatchUser)
			// 直接改绑手机号 / 邮箱（管理员操作，不校验验证码）；两者均唯一
			adminG.PATCH("/users/:id/phone", RequireScope("users:write"), h.AdminSetPhone)
			adminG.PATCH("/users/:id/email", RequireScope("users:write"), h.AdminSetEmail)
			adminG.POST("/users/:id/password", RequireScope("users:write"), h.AdminResetPassword)
			// 注销用户（墓碑式删除）不可逆，限超管
			adminG.DELETE("/users/:id", RequireSuperAdmin(), h.AdminDeleteUser)
			adminG.GET("/roles", RequireScope("users:read"), h.AdminRoles)
			// 角色管理：自定义角色可增删改；系统内置角色（superadmin/admin/user）不可删
			adminG.POST("/roles", RequireScope("users:write"), h.AdminCreateRole)
			adminG.PATCH("/roles/:id", RequireScope("users:write"), h.AdminPatchRole)
			adminG.DELETE("/roles/:id", RequireScope("users:write"), h.AdminDeleteRole)
			adminG.GET("/clients", RequireScope("users:write"), h.AdminClients)
			adminG.POST("/clients", RequireSuperAdmin(), h.AdminCreateClient)
			adminG.PATCH("/clients/:id", RequireSuperAdmin(), h.AdminPatchClient)
			// 删除应用会解绑所有引用它的会话（会话本身保留），不可逆，限超管
			adminG.DELETE("/clients/:id", RequireSuperAdmin(), h.AdminDeleteClient)
			adminG.GET("/api-keys", RequireSuperAdmin(), h.AdminAPIKeys)
			adminG.POST("/api-keys", RequireSuperAdmin(), h.AdminCreateAPIKey)
			adminG.PATCH("/api-keys/:id", RequireSuperAdmin(), h.AdminPatchAPIKey)
			adminG.DELETE("/api-keys/:id", RequireSuperAdmin(), h.AdminDeleteAPIKey)
			// 法务文档是站点级全局配置，与 clients 一样限超管
			adminG.GET("/legal", RequireSuperAdmin(), h.AdminLegal)
			adminG.PUT("/legal/:kind", RequireSuperAdmin(), h.AdminUpdateLegal)
			adminG.GET("/audit-logs", RequireScope("users:read"), h.AdminAudit)
		}

		// 业务端读取用户资料：仅 API Key（scope users:read）可调，返回脱敏资料，
		// 角色折叠成 is_admin。响应刻意不含明文手机/邮箱——那是 /v1/internal 的职责。
		usersG := v1.Group("/users")
		usersG.Use(Authenticate(cfg, authSvc), RequireScope("users:read"))
		{
			usersG.GET("", h.UsersQuery)
			usersG.GET("/:id", h.UserProfile)
		}

		internal := v1.Group("/internal")
		internal.Use(Authenticate(cfg, authSvc), RequireScope("users:read"))
		{
			// 走 profile 缓存；响应形状与历史上一致（明文 + 完整 roles），只是读路径变了。
			internal.GET("/users/:id", h.InternalUser)
			internal.GET("/users", h.InternalLookup)
		}
	}
	return r
}
