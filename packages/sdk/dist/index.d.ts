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
    user: User;
};
export type Envelope<T> = {
    code: string;
    message: string;
    data: T;
};
export declare const DAUTH_API_PREFIX = "/v1";
export declare const DAUTH_JWKS_PATH = "/v1/.well-known/jwks.json";
export declare class DAuthClient {
    readonly baseURL: string;
    readonly mode: DAuthMode;
    readonly clientKey: string;
    readonly apiKey: string;
    private getAccessToken;
    private setAccessToken;
    private onUnauthorized?;
    constructor(opts: DAuthClientOptions);
    /**
     * 注册会话失效回调。可在客户端创建之后再设置，方便宿主等 router / store
     * 就绪后再注册（避免 client 与 router 的循环依赖）。
     */
    setUnauthorizedHandler(fn: () => void): void;
    jwksURL(): string;
    persistTokens(res: TokenResult): void;
    clearTokens(): void;
    private hasRefreshCredential;
    private request;
    sendSms(phone: string, scene?: string): Promise<{
        ok: boolean;
    }>;
    /**
     * 给「当前登录用户已绑定」的手机号发验证码，号码由服务端解析。
     *
     * /v1/me 返回的手机号是脱敏的，前端无法用它调 sendSms，注销前验证等场景用这个。
     * 当前 scene 只用于 delete_account。
     */
    sendSelfSms(scene: string): Promise<{
        ok: boolean;
    }>;
    smsLogin(phone: string, code: string): Promise<TokenResult>;
    sendEmail(email: string, scene?: string): Promise<{
        ok: boolean;
    }>;
    emailRegister(email: string, code: string, password: string, nickname?: string): Promise<TokenResult>;
    /** 密码登录：identifier 可传手机号或邮箱。 */
    passwordLogin(identifier: string, password: string): Promise<TokenResult>;
    /** 仅邮箱的旧方法，保留兼容；新代码请用 passwordLogin。 */
    emailLogin(email: string, password: string): Promise<TokenResult>;
    forgotPassword(email: string): Promise<{
        ok: boolean;
    }>;
    resetPassword(email: string, code: string, password: string): Promise<{
        ok: boolean;
    }>;
    refresh(): Promise<TokenResult>;
    logout(): Promise<void>;
    wechatQrStart(): Promise<{
        ticket: string;
        qr_url: string;
    }>;
    /**
     * 注销当前账号（不可逆）。confirm 必须为「确认注销」，服务端会校验。
     *
     * 已绑定手机号的用户还须传 code：当前绑定手机号收到的验证码（delete_account
     * 场景）。未绑定手机号时忽略 code。
     *
     * 只在成功时清除本地凭据：失败（如确认串写错、验证码不对）不应把用户登出，
     * 否则会让人误以为账号已经注销。
     */
    deleteAccount(confirm: string, code?: string): Promise<{
        ok: boolean;
    }>;
    wechatQrStatus(ticket: string): Promise<{
        status: string;
        tokens?: TokenResult;
    }>;
    wechatMockScan(ticket: string): Promise<{
        ok: boolean;
    }>;
    wechatMini(clientKey: string, code: string): Promise<TokenResult>;
    me(): Promise<User>;
    /**
     * 登录探针：未登录返回空对象（`{}`），已登录返回当前用户与当前会话。
     * 不会返回 token，也不会因未登录报错，适合替代 `me()` 做启动时的登录态判断。
     */
    session(): Promise<{
        user?: User;
        session?: Session;
    }>;
    patchMe(body: {
        nickname?: string;
        bio?: string;
        gender?: number;
    }): Promise<User>;
    changePassword(new_password: string): Promise<unknown>;
    /** 绑定或更换手机号。只需新手机号的验证码（scene 用 bind_phone）。 */
    changePhone(body: {
        new_phone: string;
        new_code: string;
    }): Promise<unknown>;
    bindEmail(email: string, code: string): Promise<unknown>;
    identities(): Promise<Identity[]>;
    deleteIdentity(id: string): Promise<unknown>;
    sessions(): Promise<Session[]>;
    deleteSession(id: string): Promise<unknown>;
    signAvatar(content_type: string): Promise<{
        object_key: string;
        upload_url: string;
        public_url: string;
        headers: Record<string, string>;
    }>;
    confirmAvatar(object_key: string): Promise<User>;
    uploadAvatar(file: File): Promise<User>;
    /**
     * 后台用户列表。created_from / created_to 是 RFC3339 时间范围（端点开区间，
     * 结束时间通常传「次日 0 点」以包含当天）；sort 支持 created_at /
     * last_login_at / nickname，order 为 asc | desc。
     */
    adminUsers(params?: {
        q?: string;
        status?: string;
        role?: string;
        page?: number;
        size?: number;
        created_from?: string;
        created_to?: string;
        sort?: string;
        order?: string;
    }): Promise<{
        items: User[];
        total: number;
        page: number;
        size: number;
    }>;
    /** 用户详情。会话不在此返回，用 adminUserSessions 单独分页获取。 */
    adminUser(id: string): Promise<{
        user: User;
        identities: Identity[];
    }>;
    /**
     * 用户登录会话，分页返回。后台是审计视角：已失效/已下线的会话同样返回，
     * 用 revoked_at 区分状态。
     */
    adminUserSessions(id: string, params?: {
        page?: number;
        size?: number;
    }): Promise<{
        items: Session[];
        total: number;
        page: number;
        size: number;
    }>;
    /**
     * 后台建号接口。手机号走后台宽松校验，允许 120 开头的虚拟号等非真实号段
     * （登录/注册仍然只认真实号码）。
     */
    adminCreateUser(body: {
        nickname?: string;
        phone?: string;
        email?: string;
        password?: string;
        roles?: string[];
    }): Promise<User>;
    adminPatchUser(id: string, body: {
        nickname?: string;
        status?: string;
        roles?: string[];
    }): Promise<User>;
    /**
     * 生成一个虚拟手机号（120 开头，11 位）用于后台建号，返回 `{ phone }`。
     * 只返回候选值、不写库；该号码不能用于登录注册与收短信。
     */
    adminGenerateVirtualPhone(): Promise<{
        phone: string;
    }>;
    /** 直接改绑用户手机号（管理员操作，不校验验证码）。手机号唯一，冲突返回 PHONE_TAKEN。 */
    adminSetUserPhone(id: string, phone: string): Promise<User>;
    /** 直接改绑用户邮箱（管理员操作，不校验验证码）。邮箱唯一，冲突返回 EMAIL_TAKEN。 */
    adminSetUserEmail(id: string, email: string): Promise<User>;
    adminResetPassword(id: string, password: string): Promise<unknown>;
    /** 超管注销指定用户（墓碑式删除，不可逆）。confirm 需为「确认注销」。 */
    adminDeleteUser(id: string, confirm: string): Promise<{
        ok: boolean;
    }>;
    adminRoles(): Promise<Role[]>;
    /** 公开读取用户协议 / 隐私政策（登录页未登录时也会调用）。 */
    legal(): Promise<LegalDocSet>;
    adminLegal(): Promise<LegalDocSet>;
    /** 写入某个 kind 的链接与正文。两者都传空即回到「未设置」。 */
    adminUpdateLegal(kind: "terms" | "privacy", body: {
        url?: string;
        content?: string;
    }): Promise<LegalDoc>;
    /** 新建自定义角色。code 只能用 2-32 位小写字母、数字、下划线或连字符，且以字母开头。 */
    adminCreateRole(body: {
        code: string;
        name: string;
        description?: string;
    }): Promise<Role>;
    /** 修改角色名称/说明。code 不可改（业务系统与 JWT 都引用它）。 */
    adminUpdateRole(id: string, body: {
        name?: string;
        description?: string;
    }): Promise<Role>;
    /** 删除自定义角色。系统内置角色会返回 SYSTEM_ROLE。 */
    adminDeleteRole(id: string): Promise<{
        ok: boolean;
    }>;
    adminClients(): Promise<ClientApp[]>;
    adminCreateClient(body: Partial<ClientApp> & {
        wechat_app_secret?: string;
    }): Promise<ClientApp>;
    adminPatchClient(id: string, body: Partial<ClientApp> & {
        wechat_app_secret?: string;
    }): Promise<ClientApp>;
    /** 删除应用。控制台自身（web_console）会返回 CLIENT_PROTECTED。 */
    adminDeleteClient(id: string): Promise<{
        ok: boolean;
    }>;
    adminApiKeys(): Promise<ApiKey[]>;
    adminCreateApiKey(body: {
        name: string;
        days?: number;
    }): Promise<{
        api_key: ApiKey;
        secret: string;
    }>;
    adminPatchApiKey(id: string, body: {
        name?: string;
        status?: string;
    }): Promise<ApiKey>;
    /** 删除 API Key（物理删除，密钥立即失效且不再出现在列表里）。 */
    adminDeleteApiKey(id: string): Promise<{
        ok: boolean;
    }>;
    /** 其它后端用 API Key 按用户 ID 查资料 */
    internalUser(id: string): Promise<User>;
    /** 其它后端用 API Key 按手机号或邮箱查资料 */
    internalLookup(params: {
        phone?: string;
        email?: string;
    }): Promise<User>;
    /**
     * 业务端读用户资料（手机号/邮箱已脱敏，角色折叠成 is_admin）。
     * 需要 scope 含 users:read 的 API Key：请用 createDAuthServiceClient。
     */
    userProfile(id: string): Promise<UserProfile>;
    /**
     * 批量取资料或按手机号/邮箱精确匹配。ids/phone/email 三选一。
     * 批量时查不到的 id 会被跳过（不会报错），返回值顺序与 ids 一致。
     * 匹配不上返回空数组而不是抛错，便于「这个手机号注册过没有」这类判断。
     */
    users(params: {
        ids?: string[];
        phone?: string;
        email?: string;
    }): Promise<UserProfile[]>;
    adminAudit(params?: {
        q?: string;
        page?: number;
        size?: number;
    }): Promise<{
        items: AuditLog[];
        total: number;
    }>;
    /** 仪表盘概览：用户总数 / 活跃 / 今日与昨日注册。「今日」按服务端本地时区。 */
    adminStats(): Promise<UserStats>;
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
export declare function createDAuthClient(opts: DAuthClientOptions): DAuthClient;
/** Node / 其它后端：用 sk- API Key 调 /v1/internal 与具备 scope 的管理接口 */
export declare function createDAuthServiceClient(opts: {
    baseURL: string;
    apiKey: string;
}): DAuthClient;
export declare function isApiError(err: unknown): err is ApiError;
//# sourceMappingURL=index.d.ts.map