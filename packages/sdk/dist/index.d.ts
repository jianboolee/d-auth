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
    constructor(opts: DAuthClientOptions);
    jwksURL(): string;
    persistTokens(res: TokenResult): void;
    clearTokens(): void;
    private hasRefreshCredential;
    private request;
    sendSms(phone: string, scene?: string): Promise<{
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
    adminUsers(params?: {
        q?: string;
        status?: string;
        role?: string;
        page?: number;
        size?: number;
    }): Promise<{
        items: User[];
        total: number;
        page: number;
        size: number;
    }>;
    adminUser(id: string): Promise<{
        user: User;
        identities: Identity[];
        sessions: Session[];
    }>;
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
    /** 直接改绑用户手机号（管理员操作，不校验验证码）。手机号唯一，冲突返回 PHONE_TAKEN。 */
    adminSetUserPhone(id: string, phone: string): Promise<User>;
    /** 直接改绑用户邮箱（管理员操作，不校验验证码）。邮箱唯一，冲突返回 EMAIL_TAKEN。 */
    adminSetUserEmail(id: string, email: string): Promise<User>;
    adminResetPassword(id: string, password: string): Promise<unknown>;
    adminRoles(): Promise<Role[]>;
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
    adminApiKeys(): Promise<ApiKey[]>;
    adminCreateApiKey(body: {
        name: string;
        scopes?: string[];
        days?: number;
    }): Promise<{
        api_key: ApiKey;
        secret: string;
    }>;
    adminPatchApiKey(id: string, body: {
        status?: string;
        scopes?: string[];
    }): Promise<ApiKey>;
    /** 删除接口密钥（物理删除，密钥立即失效且不再出现在列表里）。 */
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
    adminAudit(params?: {
        q?: string;
        page?: number;
        size?: number;
    }): Promise<{
        items: AuditLog[];
        total: number;
    }>;
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
export declare function createDAuthClient(opts: DAuthClientOptions): DAuthClient;
/** Node / 其它后端：用 dak_live_ API Key 调 /v1/internal 与具备 scope 的管理接口 */
export declare function createDAuthServiceClient(opts: {
    baseURL: string;
    apiKey: string;
}): DAuthClient;
export declare function isApiError(err: unknown): err is ApiError;
//# sourceMappingURL=index.d.ts.map