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
	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/contextx"
	"github.com/jianboolee/d-auth/internal/idgen"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/admin"
	"github.com/jianboolee/d-auth/internal/modules/auth"
	"github.com/jianboolee/d-auth/internal/modules/dto"
	"github.com/jianboolee/d-auth/internal/modules/usersvc"
	"github.com/jianboolee/d-auth/internal/normalize"
	"github.com/jianboolee/d-auth/internal/response"
)

type Handlers struct {
	Cfg   *config.Config
	Auth  *auth.Service
	Users *usersvc.Service
	Admin *admin.Service
}

func (h *Handlers) setSessionCookie(c *gin.Context, sessionID uuid.UUID) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.Cfg.CookieName, sessionID.String(), int(h.Cfg.JWTRefreshTTL.Seconds()), "/", h.Cfg.CookieDomain, h.Cfg.CookieSecure, true)
}

func (h *Handlers) clearCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(h.Cfg.CookieName, "", -1, "/", h.Cfg.CookieDomain, h.Cfg.CookieSecure, true)
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
	if _, err := h.Auth.SendSMS(c, req.Phone, req.Scene); err != nil {
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
	tokens, sess, err := h.Auth.SMSLogin(c, req.Phone, req.Code, h.meta(c))
	if err != nil {
		WriteError(c, err)
		return
	}
	h.setSessionCookie(c, sess.ID)
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
	if _, err := h.Auth.SendEmail(c, req.Email, req.Scene); err != nil {
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
	tokens, sess, err := h.Auth.EmailRegister(c, req.Email, req.Code, req.Password, req.Nickname, h.meta(c))
	if err != nil {
		WriteError(c, err)
		return
	}
	h.setSessionCookie(c, sess.ID)
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
	tokens, sess, err := h.Auth.PasswordLogin(c, req.Identifier, req.Password, h.meta(c))
	if err != nil {
		WriteError(c, err)
		return
	}
	h.setSessionCookie(c, sess.ID)
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
	tokens, sess, err := h.Auth.EmailLogin(c, req.Email, req.Password, h.meta(c))
	if err != nil {
		WriteError(c, err)
		return
	}
	h.setSessionCookie(c, sess.ID)
	response.OK(c, tokens)
}

func (h *Handlers) ForgotPassword(c *gin.Context) {
	var req sendEmailReq
	if !response.BindJSON(c, &req) {
		return
	}
	if err := h.Auth.ForgotPassword(c, req.Email); err != nil {
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

func (h *Handlers) Refresh(c *gin.Context) {
	var req refreshReq
	_ = c.ShouldBindJSON(&req)
	var sid uuid.UUID
	if ck, err := c.Cookie(h.Cfg.CookieName); err == nil {
		sid, _ = uuid.Parse(ck)
	}
	tokens, sess, err := h.Auth.Refresh(req.RefreshToken, sid, h.meta(c))
	if err != nil {
		WriteError(c, err)
		return
	}
	h.setSessionCookie(c, sess.ID)
	response.OK(c, tokens)
}

func (h *Handlers) Logout(c *gin.Context) {
	if ck, err := c.Cookie(h.Cfg.CookieName); err == nil {
		if id, err := uuid.Parse(ck); err == nil {
			_ = h.Auth.Logout(id)
		}
	}
	if a, ok := contextx.GetActor(c); ok && a.SessionID != uuid.Nil {
		_ = h.Auth.Logout(a.SessionID)
	}
	h.clearCookie(c)
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
	payload, tokens, sess, err := h.Auth.WeChatQRStatus(c, c.Query("ticket"))
	if err != nil {
		WriteError(c, err)
		return
	}
	if tokens != nil {
		h.setSessionCookie(c, sess.ID)
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
	tokens, sess, err := h.Auth.WeChatMini(c, req.ClientKey, req.Code, h.meta(c))
	if err != nil {
		WriteError(c, err)
		return
	}
	h.setSessionCookie(c, sess.ID)
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

type sessionResp struct {
	User    *dto.User    `json:"user,omitempty"`
	Session *dto.Session `json:"session,omitempty"`
}

// Session 是登录探针：未登录返回 200 空对象，已登录返回当前用户与当前会话
// （设备/IP/过期时间，不含任何 token）。前端用它替代 /me 判断登录态，
// 未登录时不再产生 401。
func (h *Handlers) Session(c *gin.Context) {
	actor, sess, err := resolveActor(c, h.Cfg, h.Auth)
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
	out := sessionResp{User: &u}
	if sess != nil {
		s := dto.FromSession(sess)
		out.Session = &s
	}
	response.OK(c, out)
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
	if err := h.Users.ChangePassword(a.UserID, req.NewPassword, a.SessionID); err != nil {
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

func (h *Handlers) Sessions(c *gin.Context) {
	a, _ := contextx.GetActor(c)
	list, err := h.Users.Sessions(a.UserID)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, dto.FromSessions(list))
}

func (h *Handlers) DeleteSession(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	a, _ := contextx.GetActor(c)
	if err := h.Users.RevokeSession(a.UserID, id); err != nil {
		WriteError(c, err)
		return
	}
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
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 2<<20))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_BODY", "读取失败")
		return
	}
	u, err := h.Users.SaveAvatarBytes(a.UserID, key, body)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, u)
}

func (h *Handlers) AdminUsers(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	list, total, err := h.Admin.ListUsers(admin.UserListQuery{
		Q: c.Query("q"), Status: c.Query("status"), Role: c.Query("role"), Page: page, Size: size,
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
	u, idents, sess, err := h.Admin.GetUser(id)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"user": u, "identities": dto.FromIdentities(idents), "sessions": dto.FromSessions(sess)})
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
	// dto 对外给的是 16 位串，审计表里仍存 uuid 字节，这里还原一次。
	if target, perr := idgen.ParseUserID(u.ID); perr == nil {
		h.audit(c, "user.create", &target, nil)
	}
	response.OK(c, u)
}

type patchUserReq struct {
	Nickname *string  `json:"nickname"`
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
	u, err := h.Admin.PatchUser(id, req.Nickname, req.Status, req.Roles)
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
	response.OK(c, cl)
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
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
	Days   int      `json:"days"`
}

func (h *Handlers) AdminCreateAPIKey(c *gin.Context) {
	var req createKeyReq
	if !response.BindJSON(c, &req) {
		return
	}
	a, _ := contextx.GetActor(c)
	k, secret, err := h.Admin.CreateAPIKey(req.Name, req.Scopes, a.UserID, req.Days)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"api_key": k, "secret": secret})
}

type patchKeyReq struct {
	Status *string  `json:"status"`
	Scopes []string `json:"scopes"`
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
	k, err := h.Admin.PatchAPIKey(id, req.Status, req.Scopes)
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

func (h *Handlers) InternalUser(c *gin.Context) {
	id, err := idgen.ParseUserID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	u, _, _, err := h.Admin.GetUser(id)
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
		user, err := h.Admin.FindUserByIdentity("phone", p)
		if err != nil {
			WriteError(c, err)
			return
		}
		u, _, _, err = h.Admin.GetUser(user.ID)
		if err != nil {
			WriteError(c, err)
			return
		}
	} else if email != "" {
		e, ok := normalize.Email(email)
		if !ok {
			response.Fail(c, http.StatusBadRequest, "INVALID_EMAIL", "邮箱不正确")
			return
		}
		user, err := h.Admin.FindUserByIdentity("email", e)
		if err != nil {
			WriteError(c, err)
			return
		}
		u, _, _, err = h.Admin.GetUser(user.ID)
		if err != nil {
			WriteError(c, err)
			return
		}
	} else {
		response.Fail(c, http.StatusBadRequest, "INVALID_BODY", "需要 phone 或 email")
		return
	}
	response.OK(c, u)
}

func (h *Handlers) audit(c *gin.Context, action string, target *uuid.UUID, meta map[string]any) {
	a, ok := contextx.GetActor(c)
	if !ok {
		return
	}
	var actor *uuid.UUID
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
