package httpx

import (
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/contextx"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/admin"
	"github.com/jianboolee/d-auth/internal/modules/auth"
	"github.com/jianboolee/d-auth/internal/modules/dto"
	"github.com/jianboolee/d-auth/internal/modules/legal"
	"github.com/jianboolee/d-auth/internal/modules/profile"
	"github.com/jianboolee/d-auth/internal/modules/usersvc"
	"github.com/jianboolee/d-auth/internal/normalize"
	"github.com/jianboolee/d-auth/internal/response"
)

type Handlers struct {
	Cfg   *config.Config
	Auth  *auth.Service
	Users *usersvc.Service
	Admin *admin.Service
	// 字段名不用 Legal：会与下面的 public handler 方法 Legal() 冲突。
	LegalDocs *legal.Service
	// Profiles 是缓存优先的用户资料读取，供 /v1/users 与 /v1/internal 共用。
	Profiles *profile.Service
}

// 两枚 cookie 分工明确：
//
//   - access（CookieName，默认 dauth_access）= RS256 JWT，Path=/，跨子域可见。
//     业务后端与 d-auth 都靠它「验签即认人」，所以它必须能被业务读到。
//   - refresh（CookieRefreshName，默认 dauth_refresh）= 不透明随机串，
//     Path 限制在 CookieRefreshPath（默认 /v1/auth）。业务系统任何路径都不匹配，
//     浏览器根本不会携带它，因此 refresh 永远不外流到业务侧。
//
// 两者都是 HttpOnly（JS 读不到）、SameSite=Lax。
func setAuthCookies(c *gin.Context, cfg *config.Config, access, refresh string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cfg.CookieName, access, int(cfg.JWTAccessTTL.Seconds()),
		"/", cfg.CookieDomain, cfg.CookieSecure, true)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(cfg.CookieRefreshName, refresh, int(cfg.JWTRefreshTTL.Seconds()),
		cfg.CookieRefreshPath, cfg.CookieDomain, cfg.CookieSecure, true)
}

func (h *Handlers) setAuthCookies(c *gin.Context, access, refresh string) {
	setAuthCookies(c, h.Cfg, access, refresh)
}

// clearAuthCookies 清除两枚 cookie。删除时 Path 必须与设置时一致，否则浏览器
// 会当作不同 cookie 而删不掉——两枚 cookie 的 Path 不同，这里各清各的。
func (h *Handlers) clearAuthCookies(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.Cfg.CookieName, "", -1, "/", h.Cfg.CookieDomain, h.Cfg.CookieSecure, true)
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.Cfg.CookieRefreshName, "", -1, h.Cfg.CookieRefreshPath, h.Cfg.CookieDomain, h.Cfg.CookieSecure, true)
}

func (h *Handlers) meta(c *gin.Context) auth.LoginMeta {
	return auth.LoginMeta{
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		ClientKey: auth.ClientKeyFrom(c.GetHeader("X-DAuth-Client")),
	}
}

func (h *Handlers) Health(c *gin.Context) {
	response.OK(c, gin.H{"status": "ok"})
}

func (h *Handlers) JWKS(c *gin.Context) {
	// 公钥是公开信息；客户端（gosdk Verifier）按同一节奏缓存，轮换靠 kid。
	c.Header("Cache-Control", "public, max-age=300")
	c.JSON(http.StatusOK, h.Auth.JWKS())
}

type sendSMSReq struct {
	Phone string `json:"phone"`
	Scene string `json:"scene"`
}

func (h *Handlers) SendSMS(c *gin.Context) {
	var req sendSMSReq
	if !response.BindJSON(c, &req) {
		return
	}
	if _, err := h.Auth.SendSMS(c, req.Phone, req.Scene, c.ClientIP()); err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

type smsLoginReq struct {
	Phone string `json:"phone"`
	Code  string `json:"code"`
}

func (h *Handlers) SMSLogin(c *gin.Context) {
	var req smsLoginReq
	if !response.BindJSON(c, &req) {
		return
	}
	tokens, err := h.Auth.SMSLogin(c, req.Phone, req.Code, h.meta(c))
	if err != nil {
		WriteError(c, err)
		return
	}
	h.setAuthCookies(c, tokens.AccessToken, tokens.RefreshToken)
	response.OK(c, tokens)
}

type sendEmailReq struct {
	Email string `json:"email"`
	Scene string `json:"scene"`
}

func (h *Handlers) SendEmail(c *gin.Context) {
	var req sendEmailReq
	if !response.BindJSON(c, &req) {
		return
	}
	if _, err := h.Auth.SendEmail(c, req.Email, req.Scene, c.ClientIP()); err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

type emailRegisterReq struct {
	Email    string `json:"email"`
	Code     string `json:"code"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
}

func (h *Handlers) EmailRegister(c *gin.Context) {
	var req emailRegisterReq
	if !response.BindJSON(c, &req) {
		return
	}
	tokens, err := h.Auth.EmailRegister(c, req.Email, req.Code, req.Password, req.Nickname, h.meta(c))
	if err != nil {
		WriteError(c, err)
		return
	}
	h.setAuthCookies(c, tokens.AccessToken, tokens.RefreshToken)
	response.OK(c, tokens)
}

type passwordLoginReq struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

// PasswordLogin 支持手机号或邮箱 + 密码登录，identifier 由后端判断类型。
func (h *Handlers) PasswordLogin(c *gin.Context) {
	var req passwordLoginReq
	if !response.BindJSON(c, &req) {
		return
	}
	tokens, err := h.Auth.PasswordLogin(c, req.Identifier, req.Password, h.meta(c))
	if err != nil {
		WriteError(c, err)
		return
	}
	h.setAuthCookies(c, tokens.AccessToken, tokens.RefreshToken)
	response.OK(c, tokens)
}

type emailLoginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// EmailLogin 是仅邮箱的旧接口，保留以兼容已发布的 SDK；新接入请用 PasswordLogin。
func (h *Handlers) EmailLogin(c *gin.Context) {
	var req emailLoginReq
	if !response.BindJSON(c, &req) {
		return
	}
	tokens, err := h.Auth.EmailLogin(c, req.Email, req.Password, h.meta(c))
	if err != nil {
		WriteError(c, err)
		return
	}
	h.setAuthCookies(c, tokens.AccessToken, tokens.RefreshToken)
	response.OK(c, tokens)
}

func (h *Handlers) ForgotPassword(c *gin.Context) {
	var req sendEmailReq
	if !response.BindJSON(c, &req) {
		return
	}
	if err := h.Auth.ForgotPassword(c, req.Email, c.ClientIP()); err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

type resetReq struct {
	Email    string `json:"email"`
	Code     string `json:"code"`
	Password string `json:"password"`
}

func (h *Handlers) ResetPassword(c *gin.Context) {
	var req resetReq
	if !response.BindJSON(c, &req) {
		return
	}
	if err := h.Auth.ResetPassword(c, req.Email, req.Code, req.Password); err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh 用 refresh token 换新的 access token。refresh 来自两处：HttpOnly
// 的 dauth_refresh cookie（浏览器），或请求体 refresh_token（小程序 / App——
// 它们没有 cookie）。轮换后重设两枚 cookie，并把新 refresh 一并回给无 cookie 的客户端。
func (h *Handlers) Refresh(c *gin.Context) {
	var req refreshReq
	_ = c.ShouldBindJSON(&req)
	refresh := req.RefreshToken
	if refresh == "" {
		refresh, _ = c.Cookie(h.Cfg.CookieRefreshName)
	}
	tokens, err := h.Auth.Refresh(refresh, h.meta(c))
	if err != nil {
		WriteError(c, err)
		return
	}
	h.setAuthCookies(c, tokens.AccessToken, tokens.RefreshToken)
	response.OK(c, tokens)
}

type logoutReq struct {
	RefreshToken string `json:"refresh_token"`
}

// Logout 停止续期并清除 cookie。refresh 来自 cookie 或请求体（小程序 / App）。
// 幂等：无论 refresh 是否有效都返回成功——登出只「不再能续期」，已签发的
// access token 在 TTL 内仍可用（纯验签方案的既定窗口）。
func (h *Handlers) Logout(c *gin.Context) {
	var req logoutReq
	_ = c.ShouldBindJSON(&req)
	refresh := req.RefreshToken
	if refresh == "" {
		refresh, _ = c.Cookie(h.Cfg.CookieRefreshName)
	}
	if err := h.Auth.RevokeSession(refresh); err != nil {
		WriteError(c, err)
		return
	}
	h.clearAuthCookies(c)
	response.OK(c, gin.H{"ok": true})
}

func (h *Handlers) WeChatQRStart(c *gin.Context) {
	data, err := h.Auth.WeChatQRStart(c)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, data)
}

func (h *Handlers) WeChatQRStatus(c *gin.Context) {
	payload, tokens, err := h.Auth.WeChatQRStatus(c, c.Query("ticket"))
	if err != nil {
		WriteError(c, err)
		return
	}
	if tokens != nil {
		h.setAuthCookies(c, tokens.AccessToken, tokens.RefreshToken)
		response.OK(c, gin.H{"status": "confirmed", "tokens": tokens})
		return
	}
	response.OK(c, payload)
}

func (h *Handlers) WeChatCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	if err := h.Auth.WeChatCallback(c, code, state, h.meta(c)); err != nil {
		WriteError(c, err)
		return
	}
	c.Redirect(http.StatusFound, h.Cfg.WebOrigin+"/login?wechat=ok")
}

type miniReq struct {
	ClientKey string `json:"client_key"`
	Code      string `json:"code"`
}

func (h *Handlers) WeChatMini(c *gin.Context) {
	var req miniReq
	if !response.BindJSON(c, &req) {
		return
	}
	tokens, err := h.Auth.WeChatMini(c, req.ClientKey, req.Code, h.meta(c))
	if err != nil {
		WriteError(c, err)
		return
	}
	h.setAuthCookies(c, tokens.AccessToken, tokens.RefreshToken)
	response.OK(c, tokens)
}

type mockScanReq struct {
	Ticket string `json:"ticket"`
	OpenID string `json:"openid"`
}

func (h *Handlers) WeChatMockScan(c *gin.Context) {
	if !h.Cfg.IsDev() {
		response.Fail(c, http.StatusForbidden, "FORBIDDEN", "仅开发环境可用")
		return
	}
	var req mockScanReq
	if c.Request.Method == http.MethodGet {
		req.Ticket = c.Query("ticket")
	} else if !response.BindJSON(c, &req) {
		return
	}
	if err := h.Auth.MockWeChatConfirm(c, req.Ticket, req.OpenID); err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *Handlers) Me(c *gin.Context) {
	a, _ := contextx.GetActor(c)
	u, err := h.Users.Me(a.UserID)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, u)
}

// Legal 是公开读接口：用户协议 / 隐私政策的链接与正文。
// 登录页在未登录状态下也要能打开这两个页面，所以不能要求鉴权。
func (h *Handlers) Legal(c *gin.Context) {
	docs, err := h.LegalDocs.All()
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, docs)
}

func (h *Handlers) AdminLegal(c *gin.Context) {
	docs, err := h.LegalDocs.All()
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, docs)
}

type updateLegalReq struct {
	URL     string `json:"url"`
	Content string `json:"content"`
}

func (h *Handlers) AdminUpdateLegal(c *gin.Context) {
	kind := c.Param("kind")
	var req updateLegalReq
	if !response.BindJSON(c, &req) {
		return
	}
	doc, err := h.LegalDocs.Upsert(kind, req.URL, req.Content)
	if err != nil {
		WriteError(c, err)
		return
	}
	// 审计只记 kind，不把协议正文写进日志。
	h.audit(c, "legal.update", nil, map[string]any{"kind": kind})
	response.OK(c, doc)
}

type sessionResp struct {
	User *dto.User `json:"user,omitempty"`
}

// Session 是登录探针：未登录返回 200 空对象，已登录返回当前用户。
// 前端用它替代 /me 判断登录态，未登录时不再产生 401。
// 不再返回会话明细——会话状态已归 Redis 白名单，且不再对外暴露设备列表。
func (h *Handlers) Session(c *gin.Context) {
	actor, err := resolveActor(c, h.Cfg, h.Auth)
	if err != nil || actor == nil || actor.Kind != contextx.ActorUser {
		response.OK(c, sessionResp{})
		return
	}
	u, uerr := h.Users.Me(actor.UserID)
	if uerr != nil {
		// 用户已不可用（例如被停用/删除），按未登录处理，避免探针报错。
		response.OK(c, sessionResp{})
		return
	}
	response.OK(c, sessionResp{User: &u})
}

func (h *Handlers) PatchMe(c *gin.Context) {
	var req usersvc.PatchMe
	if !response.BindJSON(c, &req) {
		return
	}
	a, _ := contextx.GetActor(c)
	u, err := h.Users.UpdateMe(a.UserID, req)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, u)
}

type passwordReq struct {
	NewPassword string `json:"new_password"`
}

func (h *Handlers) ChangePassword(c *gin.Context) {
	var req passwordReq
	if !response.BindJSON(c, &req) {
		return
	}
	a, _ := contextx.GetActor(c)
	// 改密踢掉其它设备，但保留当前设备：把当前 refresh cookie 传下去，
	// 服务端吊销该用户除它以外的全部 refresh。无 cookie（Bearer 调用）时
	// 保留项为空，等价于踢掉全部，客户端需重新登录。
	keep, _ := c.Cookie(h.Cfg.CookieRefreshName)
	if err := h.Users.ChangePassword(a.UserID, req.NewPassword, keep); err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

type changePhoneReq struct {
	NewPhone string `json:"new_phone"`
	NewCode  string `json:"new_code"`
}

func (h *Handlers) ChangePhone(c *gin.Context) {
	var req changePhoneReq
	if !response.BindJSON(c, &req) {
		return
	}
	a, _ := contextx.GetActor(c)
	if err := h.Users.ChangePhone(c, a.UserID, req.NewPhone, req.NewCode); err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

type sendSelfSMSReq struct {
	Scene string `json:"scene"`
}

// SendSelfSMS 给当前登录用户「已绑定」的手机号发验证码。
//
// 号码取自服务端（/v1/me 返回的是脱敏号码，前端拿不到真实值）。场景仍需白名单
// 校验，当前用于 delete_account（注销前证明掌握绑定号码）。
func (h *Handlers) SendSelfSMS(c *gin.Context) {
	var req sendSelfSMSReq
	if !response.BindJSON(c, &req) {
		return
	}
	a, _ := contextx.GetActor(c)
	if _, err := h.Auth.SendSMSForUser(c, a.UserID, req.Scene, c.ClientIP()); err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

type bindEmailReq struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

func (h *Handlers) BindEmail(c *gin.Context) {
	var req bindEmailReq
	if !response.BindJSON(c, &req) {
		return
	}
	a, _ := contextx.GetActor(c)
	if err := h.Users.BindEmail(c, a.UserID, req.Email, req.Code); err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

func (h *Handlers) Identities(c *gin.Context) {
	a, _ := contextx.GetActor(c)
	list, err := h.Users.Identities(a.UserID)
	if err != nil {
		WriteError(c, err)
		return
	}
	// 自助接口：手机号/邮箱脱敏（后台的 AdminUser 仍返回明文）
	response.OK(c, dto.FromIdentitiesMasked(list))
}

func (h *Handlers) DeleteIdentity(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	a, _ := contextx.GetActor(c)
	if err := h.Users.UnbindIdentity(a.UserID, id); err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

type deleteAccountReq struct {
	Confirm string `json:"confirm"`
	// Code 是当前绑定手机号收到的验证码（delete_account 场景）。已绑手机号的
	// 用户自助注销时必须提供；未绑定手机号时忽略。管理员注销不使用这个字段。
	Code string `json:"code"`
}

// DeleteAccount 用户自助注销：需要显式确认串，已绑手机号还需短信验证码，
// 成功后清除会话 Cookie。
func (h *Handlers) DeleteAccount(c *gin.Context) {
	var req deleteAccountReq
	if !response.BindJSON(c, &req) {
		return
	}
	a, _ := contextx.GetActor(c)
	if err := h.Users.DeleteSelf(c.Request.Context(), a.UserID, req.Confirm, req.Code); err != nil {
		WriteError(c, err)
		return
	}
	// 自己就是被删对象，审计的 actor 与 target 相同；审计表没有外键，行会保留。
	h.audit(c, "user.delete", &a.UserID, nil)
	h.clearAuthCookies(c)
	response.OK(c, gin.H{"ok": true})
}

// AdminDeleteUser 超管注销指定用户（用于协助注销或处理用户请求）。
func (h *Handlers) AdminDeleteUser(c *gin.Context) {
	id, err := idgen.ParseUserID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	var req deleteAccountReq
	if !response.BindJSON(c, &req) {
		return
	}
	if err := h.Users.DeleteAccount(id, req.Confirm); err != nil {
		WriteError(c, err)
		return
	}
	h.audit(c, "user.delete", &id, map[string]any{"by": "admin"})
	response.OK(c, gin.H{"ok": true})
}

type signAvatarReq struct {
	ContentType string `json:"content_type"`
}

func (h *Handlers) SignAvatar(c *gin.Context) {
	var req signAvatarReq
	if !response.BindJSON(c, &req) {
		return
	}
	a, _ := contextx.GetActor(c)
	data, err := h.Users.SignAvatar(a.UserID, req.ContentType)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, data)
}

type confirmAvatarReq struct {
	ObjectKey string `json:"object_key"`
}

func (h *Handlers) ConfirmAvatar(c *gin.Context) {
	var req confirmAvatarReq
	if !response.BindJSON(c, &req) {
		return
	}
	a, _ := contextx.GetActor(c)
	u, err := h.Users.ConfirmAvatar(a.UserID, req.ObjectKey)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, u)
}

func (h *Handlers) UploadAvatar(c *gin.Context) {
	a, _ := contextx.GetActor(c)
	key := c.Query("key")
	// MaxBytesReader 而非 io.LimitReader：超限读取会直接报错拒绝，
	// 不会像截断那样把半张图存进对象存储。
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20))
	if err != nil {
		response.Fail(c, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "头像不能超过 2MB")
		return
	}
	u, err := h.Users.SaveAvatarBytes(a.UserID, key, body)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, u)
}

func (h *Handlers) AdminStats(c *gin.Context) {
	v, err := h.Admin.Stats()
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, v)
}

func (h *Handlers) AdminUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	list, total, err := h.Admin.ListUsers(admin.UserListQuery{
		Q: c.Query("q"), Status: c.Query("status"), Role: c.Query("role"), Page: page, Size: size,
		// 时间与排序参数原样透传，由 service 统一校验（非法值要给出可读的 400）。
		CreatedFrom: c.Query("created_from"), CreatedTo: c.Query("created_to"),
		Sort: c.Query("sort"), Order: c.Query("order"),
	})
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"items": list, "total": total, "page": page, "size": size})
}

func (h *Handlers) AdminUser(c *gin.Context) {
	id, err := idgen.ParseUserID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	u, idents, err := h.Admin.GetUser(id)
	if err != nil {
		WriteError(c, err)
		return
	}
	// 会话不在这里返回，也不再有会话列表接口（会话状态已归 Redis 白名单）。
	response.OK(c, gin.H{"user": u, "identities": dto.FromIdentities(idents)})
}

func (h *Handlers) AdminCreateUser(c *gin.Context) {
	var req admin.CreateUserInput
	if !response.BindJSON(c, &req) {
		return
	}
	u, err := h.Admin.CreateUser(req)
	if err != nil {
		WriteError(c, err)
		return
	}
	// dto 给的是字符串形式的用户 ID，审计表列是 uuid，这里转回字节。
	if target, perr := idgen.ParseUserID(u.ID); perr == nil {
		h.audit(c, "user.create", &target, nil)
	}
	response.OK(c, u)
}

type patchUserReq struct {
	Nickname *string  `json:"nickname"`
	Bio      *string  `json:"bio"`
	Gender   *int16   `json:"gender"`
	Status   *string  `json:"status"`
	Roles    []string `json:"roles"`
}

func (h *Handlers) AdminPatchUser(c *gin.Context) {
	id, err := idgen.ParseUserID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	var req patchUserReq
	if !response.BindJSON(c, &req) {
		return
	}
	u, err := h.Admin.PatchUser(id, admin.PatchUserInput{
		Nickname: req.Nickname,
		Bio:      req.Bio,
		Gender:   req.Gender,
		Status:   req.Status,
		Roles:    req.Roles,
	})
	if err != nil {
		WriteError(c, err)
		return
	}
	h.audit(c, "user.patch", &id, nil)
	response.OK(c, u)
}

type setPhoneReq struct {
	Phone string `json:"phone"`
}

// AdminSetPhone 直接修改用户手机号，不校验验证码（管理员操作）。手机号唯一。
func (h *Handlers) AdminSetPhone(c *gin.Context) {
	id, err := idgen.ParseUserID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	var req setPhoneReq
	if !response.BindJSON(c, &req) {
		return
	}
	u, err := h.Admin.SetUserIdentity(id, models.IdentityPhone, req.Phone)
	if err != nil {
		WriteError(c, err)
		return
	}
	// 审计里不记录手机号本身，避免把完整号码写进日志。
	h.audit(c, "user.set_phone", &id, nil)
	response.OK(c, u)
}

type setEmailReq struct {
	Email string `json:"email"`
}

// AdminSetEmail 直接修改用户邮箱，不校验验证码（管理员操作）。邮箱唯一。
func (h *Handlers) AdminSetEmail(c *gin.Context) {
	id, err := idgen.ParseUserID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	var req setEmailReq
	if !response.BindJSON(c, &req) {
		return
	}
	u, err := h.Admin.SetUserIdentity(id, models.IdentityEmail, req.Email)
	if err != nil {
		WriteError(c, err)
		return
	}
	h.audit(c, "user.set_email", &id, nil)
	response.OK(c, u)
}

// AdminSignAvatar 管理员为指定用户签发头像上传凭证。签名链路与自助端
// SignAvatar 完全相同，只是上传地址指向后台的 /users/:id/avatar/upload。
func (h *Handlers) AdminSignAvatar(c *gin.Context) {
	id, err := idgen.ParseUserID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	if _, _, err := h.Admin.GetUser(id); err != nil {
		WriteError(c, err)
		return
	}
	var req signAvatarReq
	if !response.BindJSON(c, &req) {
		return
	}
	data, err := h.Users.SignAvatarForAdmin(id, req.ContentType)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, data)
}

// AdminUploadAvatar 服务端中转上传：字节落在目标用户的 key 下并更新其头像。
// key 归属校验（key 必须在目标用户目录下）由 usersvc 完成，与自助端同一套规则。
func (h *Handlers) AdminUploadAvatar(c *gin.Context) {
	id, err := idgen.ParseUserID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	// 目标必须存在且未注销：Users 表是墓碑式软删，向已注销用户写头像会留下孤儿对象。
	if _, _, err := h.Admin.GetUser(id); err != nil {
		WriteError(c, err)
		return
	}
	key := c.Query("key")
	// 与自助端 UploadAvatar 同样的读取上限，防止大文件打爆内存。
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 2<<20))
	if err != nil {
		response.Fail(c, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "头像不能超过 2MB")
		return
	}
	u, err := h.Users.SaveAvatarBytes(id, key, body)
	if err != nil {
		WriteError(c, err)
		return
	}
	h.audit(c, "user.set_avatar", &id, nil)
	response.OK(c, gin.H{"avatar_url": u.AvatarURL})
}

// AdminGeneratePhone 生成一个虚拟手机号（120 开头，11 位），供后台建号表单填入。
// 只生成候选值，不写库；真正入库时仍会在 CreateUser 里再校验一次唯一性。
func (h *Handlers) AdminGeneratePhone(c *gin.Context) {
	phone, err := h.Admin.GenerateVirtualPhone()
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"phone": phone})
}

type resetPassReq struct {
	Password string `json:"password"`
}

func (h *Handlers) AdminResetPassword(c *gin.Context) {
	id, err := idgen.ParseUserID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	var req resetPassReq
	if !response.BindJSON(c, &req) {
		return
	}
	if err := h.Admin.ResetPassword(id, req.Password); err != nil {
		WriteError(c, err)
		return
	}
	h.audit(c, "user.reset_password", &id, nil)
	response.OK(c, gin.H{"ok": true})
}

func (h *Handlers) AdminRoles(c *gin.Context) {
	list, err := h.Admin.ListRoles()
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, list)
}

func (h *Handlers) AdminCreateRole(c *gin.Context) {
	var req admin.RoleInput
	if !response.BindJSON(c, &req) {
		return
	}
	role, err := h.Admin.CreateRole(req)
	if err != nil {
		WriteError(c, err)
		return
	}
	h.audit(c, "role.create", nil, map[string]any{"role_id": role.ID.String()})
	response.OK(c, role)
}

type patchRoleReq struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func (h *Handlers) AdminPatchRole(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	var req patchRoleReq
	if !response.BindJSON(c, &req) {
		return
	}
	role, err := h.Admin.UpdateRole(id, req.Name, req.Description)
	if err != nil {
		WriteError(c, err)
		return
	}
	h.audit(c, "role.patch", nil, map[string]any{"role_id": id.String()})
	response.OK(c, role)
}

func (h *Handlers) AdminDeleteRole(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	if err := h.Admin.DeleteRole(id); err != nil {
		WriteError(c, err)
		return
	}
	h.audit(c, "role.delete", nil, map[string]any{"role_id": id.String()})
	response.OK(c, gin.H{"ok": true})
}

func (h *Handlers) AdminClients(c *gin.Context) {
	list, err := h.Admin.ListClients()
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, list)
}

func (h *Handlers) AdminCreateClient(c *gin.Context) {
	var req admin.ClientInput
	if !response.BindJSON(c, &req) {
		return
	}
	cl, err := h.Admin.CreateClient(req)
	if err != nil {
		WriteError(c, err)
		return
	}
	h.audit(c, "client.create", nil, map[string]any{
		"client_id":  cl.ID.String(),
		"client_key": cl.ClientKey,
	})
	response.OK(c, cl)
}

func (h *Handlers) AdminPatchClient(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	var req admin.ClientInput
	if !response.BindJSON(c, &req) {
		return
	}
	cl, err := h.Admin.UpdateClient(id, req)
	if err != nil {
		WriteError(c, err)
		return
	}
	h.audit(c, "client.patch", nil, map[string]any{
		"client_id":  cl.ID.String(),
		"client_key": cl.ClientKey,
		"status":     cl.Status,
	})
	response.OK(c, cl)
}

func (h *Handlers) AdminDeleteClient(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	if err := h.Admin.DeleteClient(id); err != nil {
		WriteError(c, err)
		return
	}
	h.audit(c, "client.delete", nil, map[string]any{"client_id": id.String()})
	response.OK(c, gin.H{"ok": true})
}

func (h *Handlers) AdminAPIKeys(c *gin.Context) {
	list, err := h.Admin.ListAPIKeys()
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, list)
}

type createKeyReq struct {
	Name string `json:"name"`
	Days int    `json:"days"`
}

func (h *Handlers) AdminCreateAPIKey(c *gin.Context) {
	var req createKeyReq
	if !response.BindJSON(c, &req) {
		return
	}
	a, _ := contextx.GetActor(c)
	k, secret, err := h.Admin.CreateAPIKey(req.Name, a.UserID, req.Days)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"api_key": k, "secret": secret})
}

type patchKeyReq struct {
	Name   *string `json:"name"`
	Status *string `json:"status"`
}

func (h *Handlers) AdminPatchAPIKey(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	var req patchKeyReq
	if !response.BindJSON(c, &req) {
		return
	}
	k, err := h.Admin.PatchAPIKey(id, req.Name, req.Status)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, k)
}

func (h *Handlers) AdminDeleteAPIKey(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	if err := h.Admin.DeleteAPIKey(id); err != nil {
		WriteError(c, err)
		return
	}
	h.audit(c, "api_key.delete", nil, map[string]any{"api_key_id": id.String()})
	response.OK(c, gin.H{"ok": true})
}

func (h *Handlers) AdminAudit(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	list, total, err := h.Admin.ListAudit(c.Query("q"), page, size)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"items": dto.FromAuditLogs(list), "total": total, "page": page, "size": size})
}

// UserProfile 是业务端的用户详情：缓存优先，脱敏后返回，roles 折叠成 is_admin。
func (h *Handlers) UserProfile(c *gin.Context) {
	id, err := idgen.ParseUserID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	p, err := h.Profiles.ProfileByID(c.Request.Context(), id)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, p)
}

// maxQueryIDs 限制批量查询的 id 数量：单次请求不该被用来做全量枚举，
// 也避免超长 IN 查询拖垮数据库。
const maxQueryIDs = 100

// UsersQuery 是业务端的批量/匹配查询，三种用法互斥：
//   - ids=a,b,c   按用户 ID 批量取（最多 100 个，查不到的静默跳过）
//   - phone=...   按手机号精确匹配
//   - email=...   按邮箱精确匹配
//
// 统一返回 {items,total}，避免同一路径出现两种响应形状。
func (h *Handlers) UsersQuery(c *gin.Context) {
	ctx := c.Request.Context()
	idsRaw := strings.TrimSpace(c.Query("ids"))
	phone := strings.TrimSpace(c.Query("phone"))
	email := strings.TrimSpace(c.Query("email"))

	given := 0
	for _, v := range []string{idsRaw, phone, email} {
		if v != "" {
			given++
		}
	}
	if given == 0 {
		response.Fail(c, http.StatusBadRequest, "INVALID_BODY", "需要 ids、phone 或 email 之一")
		return
	}
	if given > 1 {
		response.Fail(c, http.StatusBadRequest, "INVALID_BODY", "ids、phone、email 只能提供其中一个")
		return
	}

	if idsRaw != "" {
		parts := strings.Split(idsRaw, ",")
		if len(parts) > maxQueryIDs {
			response.Fail(c, http.StatusBadRequest, "TOO_MANY_IDS", "一次最多查询 100 个用户")
			return
		}
		ids := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			id, err := idgen.ParseUserID(part)
			if err != nil {
				response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID: "+part)
				return
			}
			ids = append(ids, id)
		}
		list, err := h.Profiles.Profiles(ctx, ids)
		if err != nil {
			WriteError(c, err)
			return
		}
		response.OK(c, gin.H{"items": list, "total": len(list)})
		return
	}

	var (
		p   dto.Profile
		err error
	)
	if phone != "" {
		normalized, ok := normalize.Phone(phone)
		if !ok {
			response.Fail(c, http.StatusBadRequest, "INVALID_PHONE", "手机号不正确")
			return
		}
		p, err = h.Profiles.ProfileByIdentity(ctx, models.IdentityPhone, normalized)
	} else {
		normalized, ok := normalize.Email(email)
		if !ok {
			response.Fail(c, http.StatusBadRequest, "INVALID_EMAIL", "邮箱不正确")
			return
		}
		p, err = h.Profiles.ProfileByIdentity(ctx, models.IdentityEmail, normalized)
	}
	if err != nil {
		// 匹配不上不是错误：告诉调用方「0 条」，而不是 404——调用方用它做
		// 「这个手机号注册过没有」的判断，不该靠捕获异常来区分。
		if code, _, _ := apperr.CodeOf(err); code == "NOT_FOUND" {
			response.OK(c, gin.H{"items": []dto.Profile{}, "total": 0})
			return
		}
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"items": []dto.Profile{p}, "total": 1})
}

func (h *Handlers) InternalUser(c *gin.Context) {
	id, err := idgen.ParseUserID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	u, err := h.Profiles.UserByID(c.Request.Context(), id)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, u)
}

func (h *Handlers) InternalLookup(c *gin.Context) {
	phone := c.Query("phone")
	email := c.Query("email")
	var u dto.User
	if phone != "" {
		p, ok := normalize.Phone(phone)
		if !ok {
			response.Fail(c, http.StatusBadRequest, "INVALID_PHONE", "手机号不正确")
			return
		}
		user, err := h.Profiles.UserByIdentity(c.Request.Context(), models.IdentityPhone, p)
		if err != nil {
			WriteError(c, err)
			return
		}
		u = user
	} else if email != "" {
		e, ok := normalize.Email(email)
		if !ok {
			response.Fail(c, http.StatusBadRequest, "INVALID_EMAIL", "邮箱不正确")
			return
		}
		user, err := h.Profiles.UserByIdentity(c.Request.Context(), models.IdentityEmail, e)
		if err != nil {
			WriteError(c, err)
			return
		}
		u = user
	} else {
		response.Fail(c, http.StatusBadRequest, "INVALID_BODY", "需要 phone 或 email")
		return
	}
	response.OK(c, u)
}

func (h *Handlers) audit(c *gin.Context, action string, target *string, meta map[string]any) {
	a, ok := contextx.GetActor(c)
	if !ok {
		return
	}
	var actor *string
	var key *uuid.UUID
	if a.Kind == contextx.ActorUser {
		actor = &a.UserID
	}
	if a.Kind == contextx.ActorAPIKey {
		key = &a.APIKeyID
	}
	h.Auth.Store().WriteAudit(actor, key, target, action, c.ClientIP(), c.Request.UserAgent(), models.JSONMap(meta))
}

// StaticUploads 服务 mock 模式下本地保存的上传文件，路径形如 /static/<root>/<userID>/<name>。
func StaticUploads(root string) gin.HandlerFunc {
	return func(c *gin.Context) {
		rel := strings.TrimPrefix(c.Param("path"), "/")
		clean := path.Clean(rel)
		if clean == "." || clean == "" || strings.HasPrefix(clean, "../") || strings.Contains(clean, "..") {
			c.Status(http.StatusNotFound)
			return
		}
		full := filepath.Join(root, filepath.FromSlash(clean))
		if _, err := os.Stat(full); err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.File(full)
	}
}
