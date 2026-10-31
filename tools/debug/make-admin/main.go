// make-admin 在开发库里创建（或修复）一个固定的测试管理员账号，供自动化测试与
// 手工调试登录后台使用。
//
// 为什么不复用 seed-users：那批是「列表里的数据」，故意不设密码、只能用短信登录；
// 这个是「操作者的账号」，必须有密码且是 superadmin。两者职责不同，混在一起会
// 让 seed-users 的语义变糊。
//
// 不参与项目研发、构建、测试与部署：独立 go module，不被 api/ 或 web/ 引用。
package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/argon2"
)

// 默认凭据。密码只用于本地开发库，且账号带 admin 角色；改这里记得同步 README。
const (
	defaultLogin    = "12000000001"
	defaultPassword = "dev-admin-password"
	defaultNickname = "调试管理员"
)

// 与 api/internal/infra/password 的参数保持一致，产出的哈希格式必须能被它校验：
// argon2id$v=19$m=65536,t=1,p=4$<salt>$<hash>（base64 无填充）。
const (
	timeCost    = 1
	memoryKiB   = 64 * 1024
	parallelism = 4
	keyLen      = 32
	saltLen     = 16
)

const (
	idAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	userIDLen  = 22
)

func main() {
	var (
		dsn         = flag.String("dsn", "", "Postgres DSN（默认取 DAUTH_POSTGRES_DSN，再退回仓库根 .env）")
		login       = flag.String("login", defaultLogin, "登录标识：12 开头的虚拟号或邮箱（密码登录认 12 虚拟号）")
		password    = flag.String("password", defaultPassword, "账号密码（至少 8 位）")
		nickname    = flag.String("nickname", defaultNickname, "昵称")
		dryRun      = flag.Bool("dry-run", false, "只打印将写入的内容，不提交事务")
		allowRemote = flag.Bool("allow-remote", false, "允许连接非本机数据库（默认拒绝）")
	)
	flag.Parse()

	if len(*password) < 8 || len(*password) > 72 {
		fatal("密码长度需在 8-72 位之间")
	}
	typ, identifier, ok := parseLogin(*login)
	if !ok {
		fatal(fmt.Sprintf("登录标识 %q 不合法：用 12 开头的虚拟号或一个邮箱", *login))
	}

	resolved, source, err := resolveDSN(*dsn)
	if err != nil {
		fatal(err.Error())
	}
	if err := guardLocalOnly(resolved, *allowRemote); err != nil {
		fatal(err.Error())
	}

	hash, err := hashPassword(*password)
	if err != nil {
		fatal(fmt.Sprintf("生成密码哈希失败: %v", err))
	}

	fmt.Printf("DSN 来源: %s\n", source)
	fmt.Printf("目标库:   %s\n", describeDSN(resolved))
	fmt.Printf("账号:     %s (%s)\n", identifier, typ)
	fmt.Printf("昵称:     %s，角色: superadmin, admin, user\n\n", *nickname)

	if *dryRun {
		fmt.Println("--- dry run，不写库 ---")
		fmt.Printf("  登录 %s / 密码 %s\n", identifier, *password)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, resolved)
	if err != nil {
		fatal(fmt.Sprintf("连接数据库失败: %v", err))
	}
	defer conn.Close(ctx)

	action, err := upsertAdmin(ctx, conn, typ, identifier, *nickname, hash)
	if err != nil {
		fatal(fmt.Sprintf("写入失败（事务已回滚）: %v", err))
	}

	fmt.Printf("%s\n", action)
	fmt.Printf("登录: 标识 %s  密码 %s\n", identifier, *password)
}

// upsertAdmin 保证「这个标识对应的账号是带密码的 superadmin」。
//
// 已存在就修复（补密码、补角色、把停用的改回 active），而不是报错退出：
// 测试账号被误改状态、误清密码之后，重跑这一条就能恢复，不用手工翻库。
func upsertAdmin(ctx context.Context, conn *pgx.Conn, typ, identifier, nickname, hash string) (string, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var userID string
	err = tx.QueryRow(ctx,
		`SELECT user_id FROM auth_identities WHERE type = $1 AND identifier = $2`, typ, identifier).Scan(&userID)

	created := false
	switch {
	case err == pgx.ErrNoRows:
		userID = newUserID()
		if _, err = tx.Exec(ctx,
			`INSERT INTO auth_users (id, nickname, password_hash, status, created_at, updated_at)
			 VALUES ($1, $2, $3, 'active', now(), now())`,
			userID, nickname, hash); err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx,
			`INSERT INTO auth_identities (id, user_id, type, identifier, verified_at, created_at)
			 VALUES ($1, $2, $3, $4, now(), now())`,
			newUUID(), userID, typ, identifier); err != nil {
			return "", err
		}
		created = true
	case err != nil:
		return "", err
	default:
		// 已有账号：修回可用状态。deleted_at 一并清掉，否则软删过的号会
		// 因为唯一索引已经占用而无法重建。
		if _, err = tx.Exec(ctx,
			`UPDATE auth_users
			    SET password_hash = $2, status = 'active', deleted_at = NULL, updated_at = now()
			  WHERE id = $1`,
			userID, hash); err != nil {
			return "", err
		}
	}

	// 角色是幂等的：内置角色一定存在（0001 迁移插入）。
	if _, err = tx.Exec(ctx,
		`INSERT INTO auth_user_roles (user_id, role_id)
		 SELECT $1, id FROM auth_roles WHERE code IN ('superadmin', 'admin', 'user')
		 ON CONFLICT DO NOTHING`,
		userID); err != nil {
		return "", err
	}

	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	if created {
		return "已创建测试管理员（superadmin）", nil
	}
	return "已存在同名账号，已修复为可用状态的测试管理员（superadmin）", nil
}

// parseLogin 判断登录标识是邮箱还是虚拟号，返回身份类型与规范化后的标识。
// 与后端 PasswordIdentifier 对齐：虚拟号原样保留（不套 +86），邮箱转小写。
func parseLogin(raw string) (typ, identifier string, ok bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", false
	}
	if strings.Contains(s, "@") {
		if !strings.Contains(s, ".") {
			return "", "", false
		}
		return "email", strings.ToLower(s), true
	}
	digits := strings.ReplaceAll(strings.ReplaceAll(s, " ", ""), "-", "")
	if len(digits) < 6 || len(digits) > 15 || !strings.HasPrefix(digits, "12") {
		return "", "", false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return "", "", false
		}
	}
	return "phone", digits, true
}

// hashPassword 产出与 api/internal/infra/password.Hash 完全相同的格式。
// 那份实现的参数是硬编码常量，这里照抄一份——工具不依赖 api 模块。
func hashPassword(plain string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(plain), salt, timeCost, memoryKiB, parallelism, keyLen)
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		memoryKiB, timeCost, parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func resolveDSN(flagValue string) (dsn, source string, err error) {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v, "-dsn 参数", nil
	}
	if v := strings.TrimSpace(os.Getenv("DAUTH_POSTGRES_DSN")); v != "" {
		return v, "环境变量 DAUTH_POSTGRES_DSN", nil
	}
	path, ferr := findDotEnv()
	if ferr == nil {
		if v, ok := readEnvKey(path, "DAUTH_POSTGRES_DSN"); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v), path, nil
		}
	}
	return "", "", fmt.Errorf("找不到 DSN：用 -dsn 指定，或设置 DAUTH_POSTGRES_DSN，或在仓库根 .env 里配置")
}

func findDotEnv() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf(".env not found")
		}
		dir = parent
	}
}

func readEnvKey(path, key string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		return v, true
	}
	return "", false
}

func guardLocalOnly(dsn string, allowRemote bool) error {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("DSN 无法解析: %w", err)
	}
	switch cfg.Host {
	case "", "localhost", "127.0.0.1", "::1", "[::1]":
		return nil
	}
	if strings.HasPrefix(cfg.Host, "/") {
		return nil
	}
	if allowRemote {
		fmt.Printf("注意：已放行远程库 %s（-allow-remote）。\n\n", cfg.Host)
		return nil
	}
	return fmt.Errorf("拒绝连接非本机数据库 %q：本工具会写数据，默认只允许 localhost/127.0.0.1/::1。\n"+
		"确认目标库没问题时，加 -allow-remote 显式放行", cfg.Host)
}

func describeDSN(dsn string) string {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return "(无法解析 DSN)"
	}
	host := cfg.Host
	if host == "" {
		host = "local-socket"
	}
	return fmt.Sprintf("%s:%d/%s", host, cfg.Port, cfg.Database)
}

func newUserID() string {
	const maxAcceptable = 252
	out := make([]byte, 0, userIDLen)
	buf := make([]byte, userIDLen)
	for len(out) < userIDLen {
		if _, err := rand.Read(buf); err != nil {
			panic(fmt.Sprintf("crypto/rand failed: %v", err))
		}
		for _, b := range buf {
			if b >= maxAcceptable {
				continue
			}
			out = append(out, idAlphabet[int(b)%len(idAlphabet)])
			if len(out) == userIDLen {
				break
			}
		}
	}
	return string(out)
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for i, v := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hexdigits[v>>4], hexdigits[v&0x0f])
	}
	return string(out)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "错误: "+msg)
	os.Exit(1)
}