CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
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

CREATE INDEX IF NOT EXISTS users_status_idx ON users (status);
CREATE INDEX IF NOT EXISTS users_created_at_idx ON users (created_at DESC);

CREATE TABLE IF NOT EXISTS identities (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users (id),
    type TEXT NOT NULL,
    identifier TEXT NOT NULL,
    extra JSONB NOT NULL DEFAULT '{}'::jsonb,
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS identities_type_identifier_idx ON identities (type, identifier);
CREATE INDEX IF NOT EXISTS identities_user_id_idx ON identities (user_id);

CREATE TABLE IF NOT EXISTS roles (
    id UUID PRIMARY KEY,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    is_system BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS clients (
    id UUID PRIMARY KEY,
    client_key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    allowed_origins TEXT[] NOT NULL DEFAULT '{}',
    login_methods TEXT[] NOT NULL DEFAULT '{}',
    wechat_app_id TEXT NOT NULL DEFAULT '',
    wechat_app_secret_enc TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    key_prefix TEXT NOT NULL UNIQUE,
    key_hash TEXT NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    created_by UUID REFERENCES users (id),
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sessions (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users (id),
    client_id UUID REFERENCES clients (id),
    refresh_token_hash TEXT NOT NULL,
    access_jti TEXT NOT NULL DEFAULT '',
    ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_refresh_hash_idx ON sessions (refresh_token_hash);
CREATE INDEX IF NOT EXISTS sessions_expires_at_idx ON sessions (expires_at);

CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY,
    actor_user_id UUID,
    api_key_id UUID,
    action TEXT NOT NULL,
    target_user_id UUID,
    ip TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS audit_logs_created_at_idx ON audit_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS audit_logs_target_user_idx ON audit_logs (target_user_id);

INSERT INTO roles (id, code, name, description, is_system)
VALUES
    ('01800000-0000-7000-8000-000000000001', 'superadmin', '超级管理员', '账户中心全部能力', true),
    ('01800000-0000-7000-8000-000000000002', 'admin', '管理员', '用户与角色管理', true),
    ('01800000-0000-7000-8000-000000000003', 'user', '用户', '默认登录用户', true)
ON CONFLICT (code) DO NOTHING;

INSERT INTO clients (id, client_key, name, type, allowed_origins, login_methods, status)
VALUES (
    '01800000-0000-7000-8000-000000000010',
    'web_console',
    'd-auth 控制台',
    'web',
    ARRAY['http://localhost:5173', 'http://127.0.0.1:5173'],
    ARRAY['sms', 'email', 'wechat_qr'],
    'active'
)
ON CONFLICT (client_key) DO NOTHING;
