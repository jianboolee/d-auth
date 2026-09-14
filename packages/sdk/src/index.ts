export type DAuthMode = "cookie" | "token";

export type DAuthClientOptions = {
  /** 账户中心 origin，例如 https://auth.x-rv.com 或 http://host:8680。不要带 /v1。空字符串表示同源。 */
  baseURL: string;
  mode?: DAuthMode;
  /** 对应后台 clients.client_key；Cookie 写操作必填 */
  clientKey?: string;
  /** 服务端调用用的 dak_live_...，与用户 access_token 互斥优先 */
  apiKey?: string;
  getAccessToken?: () => string | null;
  setAccessToken?: (token: string | null) => void;
};

export type ApiError = {
  code: string;
  message: string;
  status: number;
};

export type User = {
  id: string;
  nickname: string;
  avatar_url: string;
  bio: string;
  gender: number;
  status: string;
  roles: string[];
  has_password: boolean;
  /** 是否已绑定手机号。手机号是主要登录能力，未绑定时前端应引导绑定。 */
  has_phone: boolean;
  phone?: string;
  email?: string;
  last_login_at?: string | null;
  created_at: string;
};

export type TokenResult = {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
  user: User;
};

export type Envelope<T> = { code: string; message: string; data: T };

const TOKEN_KEY = "dauth_access_token";
const REFRESH_KEY = "dauth_refresh_token";

export const DAUTH_API_PREFIX = "/v1";
export const DAUTH_JWKS_PATH = "/v1/.well-known/jwks.json";

function normalizeBaseURL(raw: string): string {
  let s = raw.trim().replace(/\/+$/, "");
  if (s.endsWith("/v1")) {
    s = s.slice(0, -3);
  }
  return s;
}

export class DAuthClient {
  readonly baseURL: string;
  readonly mode: DAuthMode;
  readonly clientKey: string;
  readonly apiKey: string;
  private getAccessToken: () => string | null;
  private setAccessToken: (token: string | null) => void;

  constructor(opts: DAuthClientOptions) {
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
        if (typeof localStorage === "undefined") return;
        if (token) localStorage.setItem(TOKEN_KEY, token);
        else localStorage.removeItem(TOKEN_KEY);
      });
    if (this.mode === "cookie") {
      this.clearTokens();
    }
  }

  jwksURL() {
    return `${this.baseURL}${DAUTH_JWKS_PATH}`;
  }

  persistTokens(res: TokenResult) {
    if (this.apiKey) return;
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
    if (typeof localStorage !== "undefined") localStorage.removeItem(REFRESH_KEY);
  }

  private hasRefreshCredential(): boolean {
    if (typeof localStorage === "undefined") return false;
    return !!localStorage.getItem(REFRESH_KEY);
  }

  private async request<T>(path: string, init: RequestInit = {}, retry = true): Promise<T> {
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
    if (
      res.status === 401 &&
      retry &&
      !this.apiKey &&
      canRefresh &&
      path !== "/v1/auth/refresh" &&
      path !== "/v1/auth/logout"
    ) {
      try {
        await this.refresh();
        return this.request<T>(path, init, false);
      } catch {
        this.clearTokens();
      }
    }
    const text = await res.text();
    let parsed: Envelope<T> | { code: string; message: string } | null = null;
    try {
      parsed = text ? JSON.parse(text) : null;
    } catch {
      parsed = null;
    }
    if (!res.ok) {
      const err: ApiError = {
        code: (parsed as { code?: string } | null)?.code ?? "HTTP_ERROR",
        message: (parsed as { message?: string } | null)?.message ?? res.statusText,
        status: res.status,
      };
      throw err;
    }
    return (parsed as Envelope<T>).data;
  }

  sendSms(phone: string, scene = "login") {
    return this.request<{ ok: boolean }>("/v1/auth/sms/send", {
      method: "POST",
      body: JSON.stringify({ phone, scene }),
    });
  }

  async smsLogin(phone: string, code: string) {
    const data = await this.request<TokenResult>("/v1/auth/sms/login", {
      method: "POST",
      body: JSON.stringify({ phone, code }),
    });
    this.persistTokens(data);
    return data;
  }

  sendEmail(email: string, scene = "register") {
    return this.request<{ ok: boolean }>("/v1/auth/email/send-code", {
      method: "POST",
      body: JSON.stringify({ email, scene }),
    });
  }

  async emailRegister(email: string, code: string, password: string, nickname = "") {
    const data = await this.request<TokenResult>("/v1/auth/email/register", {
      method: "POST",
      body: JSON.stringify({ email, code, password, nickname }),
    });
    this.persistTokens(data);
    return data;
  }

  /** 密码登录：identifier 可传手机号或邮箱。 */
  async passwordLogin(identifier: string, password: string) {
    const data = await this.request<TokenResult>("/v1/auth/password/login", {
      method: "POST",
      body: JSON.stringify({ identifier, password }),
    });
    this.persistTokens(data);
    return data;
  }

  /** 仅邮箱的旧方法，保留兼容；新代码请用 passwordLogin。 */
  async emailLogin(email: string, password: string) {
    const data = await this.request<TokenResult>("/v1/auth/email/login", {
      method: "POST",
      body: JSON.stringify({ email, password }),
    });
    this.persistTokens(data);
    return data;
  }

  forgotPassword(email: string) {
    return this.request<{ ok: boolean }>("/v1/auth/password/forgot", {
      method: "POST",
      body: JSON.stringify({ email }),
    });
  }

  resetPassword(email: string, code: string, password: string) {
    return this.request<{ ok: boolean }>("/v1/auth/password/reset", {
      method: "POST",
      body: JSON.stringify({ email, code, password }),
    });
  }

  async refresh() {
    const refresh =
      typeof localStorage === "undefined" ? "" : localStorage.getItem(REFRESH_KEY) || "";
    const data = await this.request<TokenResult>(
      "/v1/auth/refresh",
      { method: "POST", body: JSON.stringify({ refresh_token: refresh }) },
      false,
    );
    this.persistTokens(data);
    return data;
  }

  async logout() {
    // 带上 refresh token：token 模式下 access token 可能已过期，服务端要靠它
    // 定位并吊销会话，否则只是本地清空、服务端会话仍然有效。
    const refresh =
      this.mode === "cookie"
        ? ""
        : typeof localStorage === "undefined"
          ? ""
          : localStorage.getItem(REFRESH_KEY) || "";
    try {
      await this.request(
        "/v1/auth/logout",
        { method: "POST", body: JSON.stringify({ refresh_token: refresh }) },
        false,
      );
    } finally {
      this.clearTokens();
    }
  }

  wechatQrStart() {
    return this.request<{ ticket: string; qr_url: string }>("/v1/auth/wechat/qr/start", {
      method: "POST",
    });
  }

  /**
   * 注销当前账号（不可逆）。confirm 必须为「确认注销」，
   * 服务端会校验。
   *
   * 只在成功时清除本地凭据：失败（如确认串写错）不应把用户登出，
   * 否则会让人误以为账号已经注销。
   */
  async deleteAccount(confirm: string) {
    const res = await this.request<{ ok: boolean }>("/v1/me", {
      method: "DELETE",
      body: JSON.stringify({ confirm }),
    });
    this.clearTokens();
    return res;
  }

  async wechatQrStatus(ticket: string) {
    const data = await this.request<{ status: string; tokens?: TokenResult }>(
      `/v1/auth/wechat/qr/status?ticket=${encodeURIComponent(ticket)}`,
    );
    if (data.tokens) this.persistTokens(data.tokens);
    return data;
  }

  wechatMockScan(ticket: string) {
    return this.request<{ ok: boolean }>("/v1/auth/wechat/mock-scan", {
      method: "POST",
      body: JSON.stringify({ ticket }),
    });
  }

  async wechatMini(clientKey: string, code: string) {
    const data = await this.request<TokenResult>("/v1/auth/wechat/mini", {
      method: "POST",
      body: JSON.stringify({ client_key: clientKey, code }),
    });
    this.persistTokens(data);
    return data;
  }

  me() {
    return this.request<User>("/v1/me");
  }

  /**
   * 登录探针：未登录返回空对象（`{}`），已登录返回当前用户与当前会话。
   * 不会返回 token，也不会因未登录报错，适合替代 `me()` 做启动时的登录态判断。
   */
  session() {
    return this.request<{ user?: User; session?: Session }>("/v1/auth/session");
  }

  patchMe(body: { nickname?: string; bio?: string; gender?: number }) {
    return this.request<User>("/v1/me", { method: "PATCH", body: JSON.stringify(body) });
  }

  changePassword(new_password: string) {
    return this.request("/v1/me/password", {
      method: "POST",
      body: JSON.stringify({ new_password }),
    });
  }

  /** 绑定或更换手机号。只需新手机号的验证码（scene 用 bind_phone）。 */
  changePhone(body: { new_phone: string; new_code: string }) {
    return this.request("/v1/me/phone/change", { method: "POST", body: JSON.stringify(body) });
  }

  bindEmail(email: string, code: string) {
    return this.request("/v1/me/email/bind", {
      method: "POST",
      body: JSON.stringify({ email, code }),
    });
  }

  identities() {
    return this.request<Identity[]>("/v1/me/identities");
  }

  deleteIdentity(id: string) {
    return this.request(`/v1/me/identities/${id}`, { method: "DELETE" });
  }

  sessions() {
    return this.request<Session[]>("/v1/me/sessions");
  }

  deleteSession(id: string) {
    return this.request(`/v1/me/sessions/${id}`, { method: "DELETE" });
  }

  signAvatar(content_type: string) {
    return this.request<{ object_key: string; upload_url: string; public_url: string; headers: Record<string, string> }>(
      "/v1/me/avatar/sign",
      { method: "POST", body: JSON.stringify({ content_type }) },
    );
  }

  confirmAvatar(object_key: string) {
    return this.request<User>("/v1/me/avatar/confirm", {
      method: "POST",
      body: JSON.stringify({ object_key }),
    });
  }

  async uploadAvatar(file: File) {
    const contentType = file.type || "image/jpeg";
    const signed = await this.signAvatar(contentType);
    // OSS 直传是跨域请求：签名 URL 自带鉴权，不需要 cookie；若带 credentials，
    // 浏览器会因 OSS 的 Access-Control-Allow-Origin: * 而拒绝整个响应。
    // 同源地址（mock 模式的 /v1/...）仍要 cookie 和 X-DAuth-Client 以通过 CSRF 校验。
    const crossOrigin = /^https?:\/\//i.test(signed.upload_url);
    const headers: Record<string, string> = { "Content-Type": contentType };
    if (!crossOrigin) headers["X-DAuth-Client"] = this.clientKey;
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

  adminUsers(params: { q?: string; status?: string; role?: string; page?: number; size?: number } = {}) {
    const qs = new URLSearchParams();
    Object.entries(params).forEach(([k, v]) => {
      if (v !== undefined && v !== "") qs.set(k, String(v));
    });
    return this.request<{ items: User[]; total: number; page: number; size: number }>(
      `/v1/admin/users?${qs.toString()}`,
    );
  }

  adminUser(id: string) {
    return this.request<{ user: User; identities: Identity[]; sessions: Session[] }>(`/v1/admin/users/${id}`);
  }

  adminCreateUser(body: { nickname?: string; phone?: string; email?: string; password?: string; roles?: string[] }) {
    return this.request<User>("/v1/admin/users", { method: "POST", body: JSON.stringify(body) });
  }

  adminPatchUser(id: string, body: { nickname?: string; status?: string; roles?: string[] }) {
    return this.request<User>(`/v1/admin/users/${id}`, { method: "PATCH", body: JSON.stringify(body) });
  }

  /** 直接改绑用户手机号（管理员操作，不校验验证码）。手机号唯一，冲突返回 PHONE_TAKEN。 */
  adminSetUserPhone(id: string, phone: string) {
    return this.request<User>(`/v1/admin/users/${id}/phone`, {
      method: "PATCH",
      body: JSON.stringify({ phone }),
    });
  }

  /** 直接改绑用户邮箱（管理员操作，不校验验证码）。邮箱唯一，冲突返回 EMAIL_TAKEN。 */
  adminSetUserEmail(id: string, email: string) {
    return this.request<User>(`/v1/admin/users/${id}/email`, {
      method: "PATCH",
      body: JSON.stringify({ email }),
    });
  }

  adminResetPassword(id: string, password: string) {
    return this.request(`/v1/admin/users/${id}/password`, {
      method: "POST",
      body: JSON.stringify({ password }),
    });
  }

  /** 超管注销指定用户（墓碑式删除，不可逆）。confirm 需为「确认注销」。 */
  adminDeleteUser(id: string, confirm: string) {
    return this.request<{ ok: boolean }>(`/v1/admin/users/${id}`, {
      method: "DELETE",
      body: JSON.stringify({ confirm }),
    });
  }

  adminRoles() {
    return this.request<Role[]>("/v1/admin/roles");
  }

  /** 公开读取用户协议 / 隐私政策（登录页未登录时也会调用）。 */
  legal() {
    return this.request<LegalDocSet>("/v1/auth/legal");
  }

  adminLegal() {
    return this.request<LegalDocSet>("/v1/admin/legal");
  }

  /** 写入某个 kind 的链接与正文。两者都传空即回到「未设置」。 */
  adminUpdateLegal(kind: "terms" | "privacy", body: { url?: string; content?: string }) {
    return this.request<LegalDoc>(`/v1/admin/legal/${kind}`, {
      method: "PUT",
      body: JSON.stringify({ url: body.url ?? "", content: body.content ?? "" }),
    });
  }

  /** 新建自定义角色。code 只能用 2-32 位小写字母、数字、下划线或连字符，且以字母开头。 */
  adminCreateRole(body: { code: string; name: string; description?: string }) {
    return this.request<Role>("/v1/admin/roles", { method: "POST", body: JSON.stringify(body) });
  }

  /** 修改角色名称/说明。code 不可改（业务系统与 JWT 都引用它）。 */
  adminUpdateRole(id: string, body: { name?: string; description?: string }) {
    return this.request<Role>(`/v1/admin/roles/${id}`, { method: "PATCH", body: JSON.stringify(body) });
  }

  /** 删除自定义角色。系统内置角色会返回 SYSTEM_ROLE。 */
  adminDeleteRole(id: string) {
    return this.request<{ ok: boolean }>(`/v1/admin/roles/${id}`, { method: "DELETE" });
  }

  adminClients() {
    return this.request<ClientApp[]>("/v1/admin/clients");
  }

  adminCreateClient(body: Partial<ClientApp> & { wechat_app_secret?: string }) {
    return this.request<ClientApp>("/v1/admin/clients", { method: "POST", body: JSON.stringify(body) });
  }

  adminPatchClient(id: string, body: Partial<ClientApp> & { wechat_app_secret?: string }) {
    return this.request<ClientApp>(`/v1/admin/clients/${id}`, { method: "PATCH", body: JSON.stringify(body) });
  }

  adminApiKeys() {
    return this.request<ApiKey[]>("/v1/admin/api-keys");
  }

  adminCreateApiKey(body: { name: string; scopes?: string[]; days?: number }) {
    return this.request<{ api_key: ApiKey; secret: string }>("/v1/admin/api-keys", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  adminPatchApiKey(id: string, body: { status?: string; scopes?: string[] }) {
    return this.request<ApiKey>(`/v1/admin/api-keys/${id}`, { method: "PATCH", body: JSON.stringify(body) });
  }

  /** 删除接口密钥（物理删除，密钥立即失效且不再出现在列表里）。 */
  adminDeleteApiKey(id: string) {
    return this.request<{ ok: boolean }>(`/v1/admin/api-keys/${id}`, { method: "DELETE" });
  }

  /** 其它后端用 API Key 按用户 ID 查资料 */
  internalUser(id: string) {
    return this.request<User>(`/v1/internal/users/${id}`);
  }

  /** 其它后端用 API Key 按手机号或邮箱查资料 */
  internalLookup(params: { phone?: string; email?: string }) {
    const qs = new URLSearchParams();
    if (params.phone) qs.set("phone", params.phone);
    if (params.email) qs.set("email", params.email);
    return this.request<User>(`/v1/internal/users?${qs.toString()}`);
  }

  adminAudit(params: { q?: string; page?: number; size?: number } = {}) {
    const qs = new URLSearchParams();
    Object.entries(params).forEach(([k, v]) => {
      if (v !== undefined && v !== "") qs.set(k, String(v));
    });
    return this.request<{ items: AuditLog[]; total: number }>(`/v1/admin/audit-logs?${qs.toString()}`);
  }
}

export type Identity = {
  id: string;
  user_id: string;
  type: string;
  identifier: string;
  extra: Record<string, unknown>;
  verified_at?: string | null;
  created_at: string;
};

export type Session = {
  id: string;
  ip: string;
  /** 已归纳的设备描述，如 "Chrome 152 · macOS" */
  device: string;
  /** 原始 User-Agent，仅用于排查问题 */
  user_agent: string;
  expires_at: string;
  revoked_at?: string | null;
  created_at: string;
};

export type Role = {
  id: string;
  code: string;
  name: string;
  description: string;
  is_system: boolean;
  /** 拥有该角色的用户数；删除自定义角色前可据此评估影响。 */
  user_count: number;
  created_at: string;
};

/**
 * 法务文档（用户协议 / 隐私政策），全局一份。
 * url 非空时前端跳转到该地址；否则渲染 content（markdown）。两者都空即未设置。
 */
export type LegalDoc = {
  kind: "terms" | "privacy";
  url: string;
  content: string;
  updated_at: string;
};

export type LegalDocSet = Record<string, LegalDoc>;

export type ClientApp = {
  id: string;
  client_key: string;
  name: string;
  type: string;
  allowed_origins: string[];
  login_methods: string[];
  wechat_app_id: string;
  status: string;
};

export type ApiKey = {
  id: string;
  name: string;
  key_prefix: string;
  scopes: string[];
  status: string;
  expires_at?: string | null;
  last_used_at?: string | null;
  created_at: string;
};

export type AuditLog = {
  id: string;
  actor_user_id?: string | null;
  api_key_id?: string | null;
  action: string;
  target_user_id?: string | null;
  ip: string;
  created_at: string;
};

export function createDAuthClient(opts: DAuthClientOptions) {
  return new DAuthClient(opts);
}

/** Node / 其它后端：用 dak_live_ API Key 调 /v1/internal 与具备 scope 的管理接口 */
export function createDAuthServiceClient(opts: { baseURL: string; apiKey: string }) {
  return new DAuthClient({
    baseURL: opts.baseURL,
    apiKey: opts.apiKey,
    mode: "token",
    clientKey: "service",
  });
}

export function isApiError(err: unknown): err is ApiError {
  return (
    !!err &&
    typeof err === "object" &&
    "code" in err &&
    "message" in err &&
    "status" in err
  );
}
