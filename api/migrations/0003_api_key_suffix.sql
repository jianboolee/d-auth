ALTER TABLE auth_api_keys
    -- API Key 的明文后缀（后 4 位），用于在列表里显示掩码，如 sk-9f2c3d…a1b2。
    -- key_hash 是单向的，老密钥无法还原后缀，因此这一列为空（前端退化为只显示前缀）。
    -- 该列不参与鉴权，仅用于展示。
    ADD COLUMN IF NOT EXISTS key_suffix TEXT NOT NULL DEFAULT '';