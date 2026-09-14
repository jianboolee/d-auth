const TOKEN_KEY = "dauth_access_token";
const REFRESH_KEY = "dauth_refresh_token";
export const DAUTH_API_PREFIX = "/v1";
export const DAUTH_JWKS_PATH = "/v1/.well-known/jwks.json";
function normalizeBaseURL(raw) {
    let s = raw.trim().replace(/\/+$/, "");
    if (s.endsWith("/v1")) {
        s = s.slice(0, -3);
    }
    return s;
}
export class DAuthClient {
    baseURL;
    mode;
    clientKey;
    apiKey;
    getAccessToken;
    setAccessToken;
    constructor(opts) {
        this.baseURL = normalizeBaseURL(opts.baseURL);
        this.apiKey = opts.apiKey?.trim() ?? "";
        this.mode = this.apiKey ? "token" : (opts.mode ?? "cookie");
        this.clientKey = opts.clientKey ?? (this.apiKey ? "service" : "web_console");
        this.getAccessToken =
            opts.getAccessToken ??
                (() => (typeof localStorage === "undefined" ? null : localStorage.getItem(TOKEN_KEY)));
        this.setAccessToken =
            opts.setAccessToken ??
                ((token) => {
                    if (typeof localStorage === "undefined")
                        return;
                    if (token)
                        localStorage.setItem(TOKEN_KEY, token);
                    else
                        localStorage.removeItem(TOKEN_KEY);
                });
        if (this.mode === "cookie") {
            this.clearTokens();
        }
    }
    jwksURL() {
        return `${this.baseURL}${DAUTH_JWKS_PATH}`;
    }
    persistTokens(res) {
        if (this.apiKey)
            return;
        // Cookie 模式以 HttpOnly session 为准，避免 localStorage Bearer 抢鉴权并触发无意义 refresh
        if (this.mode === "cookie") {
            this.clearTokens();
            return;
        }
        this.setAccessToken(res.access_token);
        if (typeof localStorage !== "undefined") {
            localStorage.setItem(REFRESH_KEY, res.refresh_token);
        }
    }
    clearTokens() {
        this.setAccessToken(null);
        if (typeof localStorage !== "undefined")
            localStorage.removeItem(REFRESH_KEY);
    }
    hasRefreshCredential() {
        if (typeof localStorage === "undefined")
            return false;
        return !!localStorage.getItem(REFRESH_KEY);
    }
    async request(path, init = {}, retry = true) {
        const headers = new Headers(init.headers);
        headers.set("X-DAuth-Client", this.clientKey);
        if (!headers.has("Content-Type") && init.body) {
            headers.set("Content-Type", "application/json");
        }
        const token = this.apiKey || this.getAccessToken();
        if (token && !headers.has("Authorization")) {
            headers.set("Authorization", `Bearer ${token}`);
        }
        const res = await fetch(`${this.baseURL}${path}`, {
            ...init,
            headers,
            credentials: this.mode === "cookie" ? "include" : "omit",
        });
        // 仅在确有可续期凭据时才 refresh，避免未登录时 me → 401 → refresh 二次空打
        const canRefresh = !!(token && !this.apiKey) || this.hasRefreshCredential();
        if (res.status === 401 &&
            retry &&
            !this.apiKey &&
            canRefresh &&
            path !== "/v1/auth/refresh" &&
            path !== "/v1/auth/logout") {
            try {
                await this.refresh();
                return this.request(path, init, false);
            }
            catch {
                this.clearTokens();
            }
        }
        const text = await res.text();
        let parsed = null;
        try {
            parsed = text ? JSON.parse(text) : null;
        }
        catch {
            parsed = null;
        }
        if (!res.ok) {
            const err = {
                code: parsed?.code ?? "HTTP_ERROR",
                message: parsed?.message ?? res.statusText,
                status: res.status,
            };
            throw err;
        }
        return parsed.data;
    }
    sendSms(phone, scene = "login") {
        return this.request("/v1/auth/sms/send", {
            method: "POST",
            body: JSON.stringify({ phone, scene }),
        });
    }
    async smsLogin(phone, code) {
        const data = await this.request("/v1/auth/sms/login", {
            method: "POST",
            body: JSON.stringify({ phone, code }),
        });
        this.persistTokens(data);
        return data;
    }
    sendEmail(email, scene = "register") {
        return this.request("/v1/auth/email/send-code", {
            method: "POST",
            body: JSON.stringify({ email, scene }),
        });
    }
    async emailRegister(email, code, password, nickname = "") {
        const data = await this.request("/v1/auth/email/register", {
            method: "POST",
            body: JSON.stringify({ email, code, password, nickname }),
        });
        this.persistTokens(data);
        return data;
    }
    /** 密码登录：identifier 可传手机号或邮箱。 */
    async passwordLogin(identifier, password) {
        const data = await this.request("/v1/auth/password/login", {
            method: "POST",
            body: JSON.stringify({ identifier, password }),
        });
        this.persistTokens(data);
        return data;
    }
    /** 仅邮箱的旧方法，保留兼容；新代码请用 passwordLogin。 */
    async emailLogin(email, password) {
        const data = await this.request("/v1/auth/email/login", {
            method: "POST",
            body: JSON.stringify({ email, password }),
        });
        this.persistTokens(data);
        return data;
    }
    forgotPassword(email) {
        return this.request("/v1/auth/password/forgot", {
            method: "POST",
            body: JSON.stringify({ email }),
        });
    }
    resetPassword(email, code, password) {
        return this.request("/v1/auth/password/reset", {
            method: "POST",
            body: JSON.stringify({ email, code, password }),
        });
    }
    async refresh() {
        const refresh = typeof localStorage === "undefined" ? "" : localStorage.getItem(REFRESH_KEY) || "";
        const data = await this.request("/v1/auth/refresh", { method: "POST", body: JSON.stringify({ refresh_token: refresh }) }, false);
        this.persistTokens(data);
        return data;
    }
    async logout() {
        try {
            await this.request("/v1/auth/logout", { method: "POST" }, false);
        }
        finally {
            this.clearTokens();
        }
    }
    wechatQrStart() {
        return this.request("/v1/auth/wechat/qr/start", {
            method: "POST",
        });
    }
    async wechatQrStatus(ticket) {
        const data = await this.request(`/v1/auth/wechat/qr/status?ticket=${encodeURIComponent(ticket)}`);
        if (data.tokens)
            this.persistTokens(data.tokens);
        return data;
    }
    wechatMockScan(ticket) {
        return this.request("/v1/auth/wechat/mock-scan", {
            method: "POST",
            body: JSON.stringify({ ticket }),
        });
    }
    async wechatMini(clientKey, code) {
        const data = await this.request("/v1/auth/wechat/mini", {
            method: "POST",
            body: JSON.stringify({ client_key: clientKey, code }),
        });
        this.persistTokens(data);
        return data;
    }
    me() {
        return this.request("/v1/me");
    }
    /**
     * 登录探针：未登录返回空对象（`{}`），已登录返回当前用户与当前会话。
     * 不会返回 token，也不会因未登录报错，适合替代 `me()` 做启动时的登录态判断。
     */
    session() {
        return this.request("/v1/auth/session");
    }
    patchMe(body) {
        return this.request("/v1/me", { method: "PATCH", body: JSON.stringify(body) });
    }
    changePassword(new_password) {
        return this.request("/v1/me/password", {
            method: "POST",
            body: JSON.stringify({ new_password }),
        });
    }
    /** 绑定或更换手机号。只需新手机号的验证码（scene 用 bind_phone）。 */
    changePhone(body) {
        return this.request("/v1/me/phone/change", { method: "POST", body: JSON.stringify(body) });
    }
    bindEmail(email, code) {
        return this.request("/v1/me/email/bind", {
            method: "POST",
            body: JSON.stringify({ email, code }),
        });
    }
    identities() {
        return this.request("/v1/me/identities");
    }
    deleteIdentity(id) {
        return this.request(`/v1/me/identities/${id}`, { method: "DELETE" });
    }
    sessions() {
        return this.request("/v1/me/sessions");
    }
    deleteSession(id) {
        return this.request(`/v1/me/sessions/${id}`, { method: "DELETE" });
    }
    signAvatar(content_type) {
        return this.request("/v1/me/avatar/sign", { method: "POST", body: JSON.stringify({ content_type }) });
    }
    confirmAvatar(object_key) {
        return this.request("/v1/me/avatar/confirm", {
            method: "POST",
            body: JSON.stringify({ object_key }),
        });
    }
    async uploadAvatar(file) {
        const contentType = file.type || "image/jpeg";
        const signed = await this.signAvatar(contentType);
        // OSS 直传是跨域请求：签名 URL 自带鉴权，不需要 cookie；若带 credentials，
        // 浏览器会因 OSS 的 Access-Control-Allow-Origin: * 而拒绝整个响应。
        // 同源地址（mock 模式的 /v1/...）仍要 cookie 和 X-DAuth-Client 以通过 CSRF 校验。
        const crossOrigin = /^https?:\/\//i.test(signed.upload_url);
        const headers = { "Content-Type": contentType };
        if (!crossOrigin)
            headers["X-DAuth-Client"] = this.clientKey;
        const res = await fetch(signed.upload_url, {
            method: "PUT",
            body: file,
            headers,
            credentials: crossOrigin ? "omit" : this.mode === "cookie" ? "include" : "omit",
        });
        // fetch 只在网络/CORS 失败时抛错；OSS 返回 4xx/5xx 仍会 resolve，
        // 不检查会继续 confirm 并把坏 key 写入用户资料。
        if (!res.ok) {
            throw new Error(`头像上传失败（HTTP ${res.status}）`);
        }
        return this.confirmAvatar(signed.object_key);
    }
    adminUsers(params = {}) {
        const qs = new URLSearchParams();
        Object.entries(params).forEach(([k, v]) => {
            if (v !== undefined && v !== "")
                qs.set(k, String(v));
        });
        return this.request(`/v1/admin/users?${qs.toString()}`);
    }
    adminUser(id) {
        return this.request(`/v1/admin/users/${id}`);
    }
    adminCreateUser(body) {
        return this.request("/v1/admin/users", { method: "POST", body: JSON.stringify(body) });
    }
    adminPatchUser(id, body) {
        return this.request(`/v1/admin/users/${id}`, { method: "PATCH", body: JSON.stringify(body) });
    }
    /** 直接改绑用户手机号（管理员操作，不校验验证码）。手机号唯一，冲突返回 PHONE_TAKEN。 */
    adminSetUserPhone(id, phone) {
        return this.request(`/v1/admin/users/${id}/phone`, {
            method: "PATCH",
            body: JSON.stringify({ phone }),
        });
    }
    /** 直接改绑用户邮箱（管理员操作，不校验验证码）。邮箱唯一，冲突返回 EMAIL_TAKEN。 */
    adminSetUserEmail(id, email) {
        return this.request(`/v1/admin/users/${id}/email`, {
            method: "PATCH",
            body: JSON.stringify({ email }),
        });
    }
    adminResetPassword(id, password) {
        return this.request(`/v1/admin/users/${id}/password`, {
            method: "POST",
            body: JSON.stringify({ password }),
        });
    }
    adminRoles() {
        return this.request("/v1/admin/roles");
    }
    /** 新建自定义角色。code 只能用 2-32 位小写字母、数字、下划线或连字符，且以字母开头。 */
    adminCreateRole(body) {
        return this.request("/v1/admin/roles", { method: "POST", body: JSON.stringify(body) });
    }
    /** 修改角色名称/说明。code 不可改（业务系统与 JWT 都引用它）。 */
    adminUpdateRole(id, body) {
        return this.request(`/v1/admin/roles/${id}`, { method: "PATCH", body: JSON.stringify(body) });
    }
    /** 删除自定义角色。系统内置角色会返回 SYSTEM_ROLE。 */
    adminDeleteRole(id) {
        return this.request(`/v1/admin/roles/${id}`, { method: "DELETE" });
    }
    adminClients() {
        return this.request("/v1/admin/clients");
    }
    adminCreateClient(body) {
        return this.request("/v1/admin/clients", { method: "POST", body: JSON.stringify(body) });
    }
    adminPatchClient(id, body) {
        return this.request(`/v1/admin/clients/${id}`, { method: "PATCH", body: JSON.stringify(body) });
    }
    adminApiKeys() {
        return this.request("/v1/admin/api-keys");
    }
    adminCreateApiKey(body) {
        return this.request("/v1/admin/api-keys", {
            method: "POST",
            body: JSON.stringify(body),
        });
    }
    adminPatchApiKey(id, body) {
        return this.request(`/v1/admin/api-keys/${id}`, { method: "PATCH", body: JSON.stringify(body) });
    }
    /** 其它后端用 API Key 按用户 ID 查资料 */
    internalUser(id) {
        return this.request(`/v1/internal/users/${id}`);
    }
    /** 其它后端用 API Key 按手机号或邮箱查资料 */
    internalLookup(params) {
        const qs = new URLSearchParams();
        if (params.phone)
            qs.set("phone", params.phone);
        if (params.email)
            qs.set("email", params.email);
        return this.request(`/v1/internal/users?${qs.toString()}`);
    }
    adminAudit(params = {}) {
        const qs = new URLSearchParams();
        Object.entries(params).forEach(([k, v]) => {
            if (v !== undefined && v !== "")
                qs.set(k, String(v));
        });
        return this.request(`/v1/admin/audit-logs?${qs.toString()}`);
    }
}
export function createDAuthClient(opts) {
    return new DAuthClient(opts);
}
/** Node / 其它后端：用 dak_live_ API Key 调 /v1/internal 与具备 scope 的管理接口 */
export function createDAuthServiceClient(opts) {
    return new DAuthClient({
        baseURL: opts.baseURL,
        apiKey: opts.apiKey,
        mode: "token",
        clientKey: "service",
    });
}
export function isApiError(err) {
    return (!!err &&
        typeof err === "object" &&
        "code" in err &&
        "message" in err &&
        "status" in err);
}
