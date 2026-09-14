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

type emailLoginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

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
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (h *Handlers) ChangePassword(c *gin.Context) {
	var req passwordReq
	if !response.BindJSON(c, &req) {
		return
	}
	a, _ := contextx.GetActor(c)
	if err := h.Users.ChangePassword(a.UserID, req.OldPassword, req.NewPassword, a.SessionID); err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"ok": true})
}

type changePhoneReq struct {
	OldPhone string `json:"old_phone"`
	OldCode  string `json:"old_code"`
	NewPhone string `json:"new_phone"`
	NewCode  string `json:"new_code"`
}

func (h *Handlers) ChangePhone(c *gin.Context) {
	var req changePhoneReq
	if !response.BindJSON(c, &req) {
		return
	}
	a, _ := contextx.GetActor(c)
	if err := h.Users.ChangePhone(c, a.UserID, req.OldPhone, req.OldCode, req.NewPhone, req.NewCode); err != nil {
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
	response.OK(c, list)
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
	response.OK(c, list)
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
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, "INVALID_ID", "无效 ID")
		return
	}
	u, idents, sess, err := h.Admin.GetUser(id)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"user": u, "identities": idents, "sessions": sess})
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
	h.audit(c, "user.create", &u.ID, nil)
	response.OK(c, u)
}

type patchUserReq struct {
	Nickname *string  `json:"nickname"`
	Status   *string  `json:"status"`
	Roles    []string `json:"roles"`
}

func (h *Handlers) AdminPatchUser(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
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

type resetPassReq struct {
	Password string `json:"password"`
}

func (h *Handlers) AdminResetPassword(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
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

func (h *Handlers) AdminAudit(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	list, total, err := h.Admin.ListAudit(c.Query("q"), page, size)
	if err != nil {
		WriteError(c, err)
		return
	}
	response.OK(c, gin.H{"items": list, "total": total, "page": page, "size": size})
}

func (h *Handlers) InternalUser(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
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
