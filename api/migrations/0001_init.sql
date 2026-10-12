CREATE TABLE IF NOT EXISTS auth_schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS auth_users (
    -- id 是 22 位小写字母数字串（NanoID 风格，见 internal/idgen），TEXT 存储。
    -- 库里的值就是对外接口返回的值，两者永远一致。
    id TEXT PRIMARY KEY,
    nickname TEXT NOT NULL DEFAULT '',
    avatar_key TEXT NOT NULL DEFAULT '',
    bio TEXT NOT NULL DEFAULT '',
    gender SMALLINT NOT NULL DEFAULT 0,
    password_hash TEXT,
    status TEXT NOT NULL DEFAULT 'active',
    last_login_at TIMESTAMPTZ,
    last_login_ip TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS auth_users_status_idx ON auth_users (status);
CREATE INDEX IF NOT EXISTS auth_users_created_at_idx ON auth_users (created_at DESC);

CREATE TABLE IF NOT EXISTS auth_identities (
    id UUID PRIMARY KEY,
    -- 用户 ID 是 TEXT（见 auth_users.id）。
    user_id TEXT NOT NULL REFERENCES auth_users (id),
    type TEXT NOT NULL,
    identifier TEXT NOT NULL,
    extra JSONB NOT NULL DEFAULT '{}'::jsonb,
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS auth_identities_type_identifier_idx ON auth_identities (type, identifier);
CREATE INDEX IF NOT EXISTS auth_identities_user_id_idx ON auth_identities (user_id);

CREATE TABLE IF NOT EXISTS auth_roles (
    id UUID PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    is_system BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS auth_user_roles (
    -- user_id 是 TEXT（见 auth_users.id）；role_id 仍是 uuid。
    user_id TEXT NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES auth_roles (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS auth_clients (
    id UUID PRIMARY KEY,
    client_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    -- 跨域来源与可用登录方式不再按应用配置：前者由环境变量
    -- DAUTH_ALLOWED_ORIGINS 统一控制，后者目前是全局的。
    wechat_app_id TEXT NOT NULL DEFAULT '',
    wechat_app_secret_enc TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS auth_api_keys (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    key_prefix TEXT NOT NULL UNIQUE,
    key_hash TEXT NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    -- created_by 是创建者用户 ID（TEXT，见 auth_users.id）；可为空。
    created_by TEXT REFERENCES auth_users (id),
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS auth_sessions (
    id UUID PRIMARY KEY,
    -- user_id 是 TEXT（见 auth_users.id）；client_id 仍是 uuid。
    user_id TEXT NOT NULL REFERENCES auth_users (id),
    client_id UUID REFERENCES auth_clients (id),
    refresh_token_hash TEXT NOT NULL,
    access_jti TEXT NOT NULL DEFAULT '',
    ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS auth_sessions_user_id_idx ON auth_sessions (user_id);
CREATE INDEX IF NOT EXISTS auth_sessions_refresh_hash_idx ON auth_sessions (refresh_token_hash);
CREATE INDEX IF NOT EXISTS auth_sessions_expires_at_idx ON auth_sessions (expires_at);

CREATE TABLE IF NOT EXISTS auth_audit_logs (
    id UUID PRIMARY KEY,
    -- actor/target 是用户 ID（TEXT），是历史快照，刻意不加外键。
    actor_user_id TEXT,
    api_key_id UUID,
    action TEXT NOT NULL,
    target_user_id TEXT,
    ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS auth_audit_logs_created_at_idx ON auth_audit_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS auth_audit_logs_target_user_idx ON auth_audit_logs (target_user_id);

INSERT INTO auth_roles (id, code, name, description, is_system)
VALUES
    ('01800000-0000-7000-8000-000000000001', 'superadmin', '超级管理员', '账户中心全部能力', true),
    ('01800000-0000-7000-8000-000000000002', 'admin', '管理员', '用户与角色管理', true),
    ('01800000-0000-7000-8000-000000000003', 'user', '用户', '默认登录用户', true)
ON CONFLICT (code) DO NOTHING;

INSERT INTO auth_clients (id, client_key, name, type, status)
VALUES (
    '01800000-0000-7000-8000-000000000010',
    'web_console',
    'd-auth 控制台',
    'web',
    'active'
)
ON CONFLICT (client_key) DO NOTHING;
