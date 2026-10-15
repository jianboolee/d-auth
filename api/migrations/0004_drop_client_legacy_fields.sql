ALTER TABLE auth_clients
    -- 跨域来源与可用登录方式已不再按应用配置：前者由环境变量
    -- DAUTH_ALLOWED_ORIGINS 统一控制，后者目前是全局的。
    --
    -- 这两列已在 6895d3c 从 0001_init.sql 中移除，但迁移器对已应用过的
    -- 文件只跑一次（见 internal/infra/database/migrate.go），所以在那之前
    -- 建好的库里这两列仍物理存在。这里补一条把它们真正删掉；新库上本就
    -- 没有这两列，IF EXISTS 让本迁移成为空操作，可安全重跑。
    DROP COLUMN IF EXISTS allowed_origins,
    DROP COLUMN IF EXISTS login_methods;