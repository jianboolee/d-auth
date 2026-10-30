export type DAuthMode = "cookie" | "token";

export type DAuthClientOptions = {
  /** 账户中心 origin，例如 https://auth.x-rv.com 或 http://host:8680。不要带 /v1。空字符串表示同源。 */
  baseURL: string;
  mode?: DAuthMode;
  /** 对应后台 clients.client_key；Cookie 写操作必填 */
  clientKey?: string;
  /** 服务端调用用的 sk-... API Key，与用户 access_token 互斥优先 */
  apiKey?: string;
  getAccessToken?: () => string | null;
  setAccessToken?: (token: string | null) => void;
  /**
   * 会话失效（401 UNAUTHORIZED）时的回调，用于把用户带回登录页。
   *
   * 只在 code === "UNAUTHORIZED" 时触发：登录接口密码错误同样返回 401，
   * 但 code 是 INVALID_CREDENTIALS，不应该让用户被"跳转"出去。
   */
  onUnauthorized?: () => void;
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
  /**
   * auth_users 行最后一次被修改的时间：改昵称/状态、重置密码、每次登录都会推进。
   * 不含改绑手机号/邮箱（那写的是 auth_identities，不推进此值）。
   */
  updated_at: string;
};

export type TokenResult = {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
  /**
   * 本次请求是否刚刚创建了该用户（不是「用户很新」）。
   * 只有真正建号的登录路径为 true：短信登录首次建号、小程序登录首次建号、
   * 邮箱注册。密码登录与续期恒为 false。
   */
  is_new_user: boolean;
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
  private onUnauthorized?: () => void;
  /** 同一时刻只允许一次续期：refresh 会轮换，多请求并发刷新会让后到的请求失败。 */
  private refreshPromise: Promise<TokenResult> | null = null;

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
    this.onUnauthorized = opts.onUnauthorized;
    if (this.mode === "cookie") {
      this.clearTokens();
    }
  }

  /**
   * 注册会话失效回调。可在客户端创建之后再设置，方便宿主等 router / store
   * 就绪后再注册（避免 client 与 router 的循环依赖）。
   */
  setUnauthorizedHandler(fn: () => void) {
    this.onUnauthorized = fn;
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
    const text = await res.text();
    let parsed: Envelope<T> | { code: string; message: string } | null = null;
    try {
      parsed = text ? JSON.parse(text) : null;
    } catch {
      parsed = null;
    }

    const code = (parsed as { code?: string } | null)?.code;
    // 只对「会话失效」续期，不能对登录密码错误（同为 401，但 code 是
    // INVALID_CREDENTIALS）也刷新，否则一次错误登录会重放成两次。
    //
    // Cookie 模式的可续期凭据是 HttpOnly cookie，JS 读不到，不能因为
    // localStorage 为空就认定「无法续期」——否则 access 到期后永远不会刷新。
    const canRefresh =
      !this.apiKey && (this.mode === "cookie" || !!token || this.hasRefreshCredential());
    if (
      res.status === 401 &&
      code === "UNAUTHORIZED" &&
      retry &&
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
    if (!res.ok) {
      const err: ApiError = {
        code: code ?? "HTTP_ERROR",
        message: (parsed as { message?: string } | null)?.message ?? res.statusText,
        status: res.status,
      };
      // 会话已失效（不是凭据错误）：通知宿主把人带回登录页。
      // 用 code 而不是 status 判定，避免把登录失败的 401 也当成掉线。
      //
      // 排除 refresh / logout：它们本身就是会话生命周期的管理动作，
      // 且可能被上面的 401 重试路径内部调用 —— 若不过滤，一次失败会重复
      // 触发回调，甚至让「登录失败」也误报成掉线。
      const isSessionEndpoint = path === "/v1/auth/refresh" || path === "/v1/auth/logout";
      if (err.status === 401 && err.code === "UNAUTHORIZED" && !isSessionEndpoint) {
        this.onUnauthorized?.();
      }
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

  /**
   * 给「当前登录用户已绑定」的手机号发验证码，号码由服务端解析。
   *
   * /v1/me 返回的手机号是脱敏的，前端无法用它调 sendSms，注销前验证等场景用这个。
   * 当前 scene 只用于 delete_account。
   */
  sendSelfSms(scene: string) {
    return this.request<{ ok: boolean }>("/v1/me/sms/self", {
      method: "POST",
      body: JSON.stringify({ scene }),
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

  refresh() {
    // 多个请求同时 401 时共用一个 refresh：服务端轮换是单次有效的，
    // 并发调用会让除第一枚之外的 refresh 全部失败，进而误判为掉线。
    if (!this.refreshPromise) {
      this.refreshPromise = this.doRefresh().finally(() => {
        this.refreshPromise = null;
      });
    }
    return this.refreshPromise;
  }

  private async doRefresh() {
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
   * 注销当前账号（不可逆）。confirm 必须为「确认注销」，服务端会校验。
   *
   * 已绑定手机号的用户还须传 code：当前绑定手机号收到的验证码（delete_account
   * 场景）。未绑定手机号时忽略 code。
   *
   * 只在成功时清除本地凭据：失败（如确认串写错、验证码不对）不应把用户登出，
   * 否则会让人误以为账号已经注销。
   */
  async deleteAccount(confirm: string, code?: string) {
    const res = await this.request<{ ok: boolean }>("/v1/me", {
      method: "DELETE",
      body: JSON.stringify({ confirm, code }),
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

  /**
   * 当前登录用户的资料。手机号/邮箱已脱敏。
   *
   * 只认用户登录态：用 `createDAuthServiceClient`（API Key）调会返回 401
   * UNAUTHORIZED —— /v1/me 要求 Actor 是用户。服务端要按用户 ID 查资料请用
   * `userProfile` / `internalUser`。
   */
  me() {
    return this.request<User>("/v1/me");
  }

  /**
   * 登录探针：未登录返回空对象（`{}`），已登录返回当前用户。
   * 不会返回 token，也不会因未登录报错，适合替代 `me()` 做启动时的登录态判断。
   *
   * 同样只认用户登录态：API Key 调用会命中 actor.Kind != user 的分支，
   * 一律返回空对象而不是用户信息 —— 别用它做服务端「查当前用户」。
   */
  session() {
    return this.request<{ user?: User }>("/v1/auth/session");
  }

  /**
   * 登录回跳校验：把 URL 上的 client 与 return_to 交给服务端白名单判定。
   * 命中返回 `{allowed:true, url}`，否则 `{allowed:false}`。公开接口，
   * 永远 200；网络/接口异常由调用方按「不允许」处理。
   */
  resolveReturnTarget(client: string, returnTo: string) {
    const q = new URLSearchParams({ client, return_to: returnTo });
    return this.request<{ allowed: boolean; url?: string }>(`/v1/auth/return-target?${q}`);
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
    await this.relayAvatarUpload(signed.upload_url, file, contentType);
    return this.confirmAvatar(signed.object_key);
  }

  /**
   * 把头像字节 PUT 到服务端中转地址（同源相对路径）。自助端与后台端共用。
   * 上传地址需要 cookie 与 X-DAuth-Client 通过鉴权与 CSRF 校验；token 模式下
   * raw fetch 不经过 request()，要自己带上与常规请求相同的 Bearer。
   */
  private async relayAvatarUpload(
    uploadUrl: string,
    file: File,
    contentType: string,
  ): Promise<unknown> {
    const crossOrigin = /^https?:\/\//i.test(uploadUrl);
    const headers: Record<string, string> = { "Content-Type": contentType };
    if (!crossOrigin) {
      headers["X-DAuth-Client"] = this.clientKey;
      const token = this.apiKey || this.getAccessToken();
      if (token) headers["Authorization"] = `Bearer ${token}`;
    }
    const res = await fetch(crossOrigin ? uploadUrl : `${this.baseURL}${uploadUrl}`, {
      method: "PUT",
      body: file,
      headers,
      credentials: crossOrigin ? "omit" : this.mode === "cookie" ? "include" : "omit",
    });
    // fetch 只在网络/CORS 失败时抛错；服务端返回 4xx/5xx 仍会 resolve，
    // 不检查会继续 confirm 并把坏 key 写入用户资料。
    if (!res.ok) {
      throw new Error(`头像上传失败（HTTP ${res.status}）`);
    }
    return res.json().catch(() => null);
  }

  /** 管理员为指定用户签发头像上传凭证，返回的 upload_url 指向后台路由。 */
  adminSignAvatar(id: string, contentType: string) {
    return this.request<{
      object_key: string;
      upload_url: string;
      public_url: string;
      headers: Record<string, string>;
    }>(`/v1/admin/users/${id}/avatar/sign`, {
      method: "POST",
      body: JSON.stringify({ content_type: contentType }),
    });
  }

  /**
   * 管理员直接更换指定用户的头像。走服务端中转（PUT 到 sign 返回的 upload_url），
   * 服务端保存字节并更新头像，返回最新的 avatar_url；无需自助端那样的 confirm。
   */
  async adminUploadAvatar(id: string, file: File) {
    const contentType = file.type || "image/jpeg";
    const signed = await this.adminSignAvatar(id, contentType);
    const data = await this.relayAvatarUpload(signed.upload_url, file, contentType);
    return (data as { avatar_url?: string } | null)?.avatar_url ?? signed.public_url;
  }

  /**
   * 后台用户列表。created_from / created_to 是 RFC3339 时间范围（端点开区间，
   * 结束时间通常传「次日 0 点」以包含当天）；sort 支持 created_at /
   * last_login_at / nickname，order 为 asc | desc。
   */
  adminUsers(params: {
    q?: string;
    status?: string;
    role?: string;
    page?: number;
    size?: number;
    created_from?: string;
    created_to?: string;
    sort?: string;
    order?: string;
  } = {}) {
    const qs = new URLSearchParams();
    Object.entries(params).forEach(([k, v]) => {
      if (v !== undefined && v !== "") qs.set(k, String(v));
    });
    return this.request<{ items: User[]; total: number; page: number; size: number }>(
      `/v1/admin/users?${qs.toString()}`,
    );
  }

  /** 用户详情。会话不再随用户返回，也不再有会话列表接口（会话状态已归 Redis 白名单）。 */
  adminUser(id: string) {
    return this.request<{ user: User; identities: Identity[] }>(`/v1/admin/users/${id}`);
  }

  /**
   * 后台建号接口。手机号走后台宽松校验，允许 120 开头的虚拟号等非真实号段
   * （登录/注册仍然只认真实号码）。
   */
  adminCreateUser(body: { nickname?: string; phone?: string; email?: string; password?: string; roles?: string[] }) {
    return this.request<User>("/v1/admin/users", { method: "POST", body: JSON.stringify(body) });
  }

  adminPatchUser(id: string, body: { nickname?: string; bio?: string; gender?: number; status?: string; roles?: string[] }) {
    return this.request<User>(`/v1/admin/users/${id}`, { method: "PATCH", body: JSON.stringify(body) });
  }

  /**
   * 生成一个虚拟手机号（120 开头，11 位）用于后台建号，返回 `{ phone }`。
   * 只返回候选值、不写库；该号码不能用于登录注册与收短信。
   */
  adminGenerateVirtualPhone() {
    return this.request<{ phone: string }>("/v1/admin/virtual-phone", { method: "POST" });
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

  /**
   * 解绑用户的某条登录标识（手机号 / 邮箱 / 微信 / 小程序），返回更新后的用户。
   * 若解绑后该用户既无其它标识也无密码，服务端返回 LAST_IDENTITY 拒绝执行。
   */
  adminDeleteIdentity(id: string, identityId: string) {
    return this.request<User>(`/v1/admin/users/${id}/identities/${identityId}`, {
      method: "DELETE",
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

  /** 删除应用。控制台自身（web_console）会返回 CLIENT_PROTECTED。 */
  adminDeleteClient(id: string) {
    return this.request<{ ok: boolean }>(`/v1/admin/clients/${id}`, { method: "DELETE" });
  }

  adminApiKeys() {
    return this.request<ApiKey[]>("/v1/admin/api-keys");
  }

  adminCreateApiKey(body: { name: string; days?: number }) {
    return this.request<{ api_key: ApiKey; secret: string }>("/v1/admin/api-keys", {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  adminPatchApiKey(id: string, body: { name?: string; status?: string }) {
    return this.request<ApiKey>(`/v1/admin/api-keys/${id}`, { method: "PATCH", body: JSON.stringify(body) });
  }

  /** 删除 API Key（物理删除，密钥立即失效且不再出现在列表里）。 */
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

  /**
   * 业务端读用户资料（手机号/邮箱已脱敏，角色折叠成 is_admin）。
   * 需要 scope 含 users:read 的 API Key：请用 createDAuthServiceClient。
   */
  userProfile(id: string) {
    return this.request<UserProfile>(`/v1/users/${id}`);
  }

  /**
   * 批量取资料或按手机号/邮箱精确匹配。ids/phone/email 三选一。
   * 批量时查不到的 id 会被跳过（不会报错），返回值顺序与 ids 一致。
   * 匹配不上返回空数组而不是抛错，便于「这个手机号注册过没有」这类判断。
   */
  async users(params: { ids?: string[]; phone?: string; email?: string }): Promise<UserProfile[]> {
    const qs = new URLSearchParams();
    if (params.ids && params.ids.length) qs.set("ids", params.ids.join(","));
    if (params.phone) qs.set("phone", params.phone);
    if (params.email) qs.set("email", params.email);
    const data = await this.request<{ items: UserProfile[]; total: number }>(
      `/v1/users?${qs.toString()}`,
    );
    return data.items;
  }

  adminAudit(params: { q?: string; page?: number; size?: number } = {}) {
    const qs = new URLSearchParams();
    Object.entries(params).forEach(([k, v]) => {
      if (v !== undefined && v !== "") qs.set(k, String(v));
    });
    return this.request<{ items: AuditLog[]; total: number }>(`/v1/admin/audit-logs?${qs.toString()}`);
  }

  /** 仪表盘概览：用户总数 / 活跃 / 今日与昨日注册。「今日」按服务端本地时区。 */
  adminStats() {
    return this.request<UserStats>("/v1/admin/stats");
  }
}

/**
 * 业务端看到的用户资料（/v1/users）。手机号/邮箱已脱敏，
 * 角色明细折叠成 is_admin —— 业务端通常只需要判断「是不是管理员」。
 */
export type UserProfile = {
  id: string;
  nickname: string;
  avatar_url: string;
  bio: string;
  gender: number;
  status: string;
  /** 是否为管理角色（admin / superadmin）。 */
  is_admin: boolean;
  has_phone: boolean;
  /** 已脱敏，如 138****8000。未绑定时不返回。 */
  phone?: string;
  /** 已脱敏，如 z***n@example.com。未绑定时不返回。 */
  email?: string;
  created_at: string;
};

export type Identity = {
  id: string;
  user_id: string;
  type: string;
  identifier: string;
  extra: Record<string, unknown>;
  verified_at?: string | null;
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
  wechat_app_id: string;
  status: string;
};

export type ApiKey = {
  id: string;
  name: string;
  key_prefix: string;
  /** 密钥明文后 4 位，仅用于列表展示掩码；老密钥为空 */
  key_suffix?: string;
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

/** 仪表盘统计。today / yesterday 指注册数；环比由前端算。 */
export type UserStats = {
  total: number;
  active: number;
  today: number;
  yesterday: number;
};

export function createDAuthClient(opts: DAuthClientOptions) {
  return new DAuthClient(opts);
}

/** Node / 其它后端：用 sk- API Key 调 /v1/internal 与具备 scope 的管理接口 */
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
