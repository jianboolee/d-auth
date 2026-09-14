CREATE TABLE IF NOT EXISTS auth_legal_docs (
    -- 站点级法务文档（用户协议 / 隐私政策），每个 kind 一行。
    -- 前端行为：url 非空 → 跳转该地址；url 空但 content 非空 → 渲染正文；
    -- 两者都空 → 保持占位页。kind 取值见 models.LegalKind*。
    -- 注意：本文件的注释必须写在语句内部。migrate.go 的 splitSQL 会丢弃任何
    -- 以 "--" 开头的分号片段，因此行首注释会导致紧随其后的语句被静默跳过。
    kind TEXT PRIMARY KEY,
    url TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO auth_legal_docs (kind, url, content)
VALUES
    ('terms', '', ''),
    ('privacy', '', '')
ON CONFLICT (kind) DO NOTHING;
