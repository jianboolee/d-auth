// seed-users 往开发库里插入一批测试用户，供后台列表/筛选/分页调试使用。
//
// 它不参与项目研发、构建、测试与部署，也不被 api/ 或 web/ 引用：本目录是
// 独立 go module（有自己的 go.mod 与 Makefile），`api/` 里的 `go build ./...`
// 不会看到它，仓库根的 Makefile 也没有指向它的目标。
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// alphabet 与 api/internal/idgen 的用户 ID 字符表一致：22 位小写字母数字。
// 这里独立实现一份，是为了让本工具不依赖 api 模块——工具不该反向拖住业务代码。
const (
	idAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	userIDLen  = 22
)

// englishNames 是 A–Z 的第一批：每个首字母一个常见英文名。
var englishNames = []string{
	"Alice", "Bob", "Charlie", "David", "Emily", "Frank", "Grace", "Henry",
	"Ivan", "Julia", "Kevin", "Laura", "Michael", "Nancy", "Oscar", "Paul",
	"Quinn", "Rachel", "Sam", "Tina", "Uma", "Victor", "Wendy", "Xavier",
	"Yvonne", "Zoe",
}

// chineseNames 是固定的 20 个中文名，覆盖常见姓氏与两字/三字名，
// 用来检查中文昵称在表格里的截断与对齐。
var chineseNames = []string{
	"张伟", "王芳", "李娜", "刘洋", "陈静",
	"杨帆", "赵磊", "黄敏", "周杰", "吴倩",
	"徐强", "孙丽", "马超", "朱婷", "胡军",
	"郭涛", "何雪", "高峰", "林静怡", "罗兰",
}

type seedUser struct {
	Nickname string
	Phone    string
	Created  time.Time
	LastSeen *time.Time
}

func main() {
	var (
		dsn         = flag.String("dsn", "", "Postgres DSN（默认取 DAUTH_POSTGRES_DSN，再退回仓库根 .env）")
		batch       = flag.Int("batch", 1, "批次号 1-99：>1 时昵称加后缀、手机号带批次，避免与上批撞号")
		spread      = flag.Duration("spread", 10*time.Minute, "同批次内相邻用户的注册时间间隔")
		batchGap    = flag.Duration("batch-gap", 24*time.Hour, "批次之间的注册时间偏移")
		dryRun      = flag.Bool("dry-run", false, "只打印将要写入的内容，不提交事务")
		allowRemote = flag.Bool("allow-remote", false, "允许连接非本机数据库（默认拒绝）")
	)
	flag.Parse()

	if *batch < 1 || *batch > 99 {
		fatal("batch 必须在 1-99 之间（手机号里占两位）")
	}

	resolved, source, err := resolveDSN(*dsn)
	if err != nil {
		fatal(err.Error())
	}
	if err := guardLocalOnly(resolved, *allowRemote); err != nil {
		fatal(err.Error())
	}

	users := buildUsers(*batch, *spread, *batchGap)

	fmt.Printf("DSN 来源: %s\n", source)
	fmt.Printf("目标库:   %s\n", describeDSN(resolved))
	fmt.Printf("批次:     %d（%d 个用户：%d 英文 + %d 中文）\n", *batch, len(users), len(englishNames), len(chineseNames))
	fmt.Println()

	if *dryRun {
		fmt.Println("--- dry run，不写库 ---")
		for _, u := range users {
			last := "-"
			if u.LastSeen != nil {
				last = u.LastSeen.Format("2006-01-02 15:04")
			}
			fmt.Printf("  %-12s %-12s 注册 %s  最近登录 %s\n",
				u.Nickname, u.Phone, u.Created.Format("2006-01-02 15:04"), last)
		}
		fmt.Printf("\n共 %d 个用户，未写入任何数据。\n", len(users))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, resolved)
	if err != nil {
		fatal(fmt.Sprintf("连接数据库失败: %v", err))
	}
	defer conn.Close(ctx)

	// 先确认表存在：连错库（比如一个空库）时给出明确提示，而不是等 INSERT 报错。
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('public.auth_users') IS NOT NULL`).Scan(&exists); err != nil {
		fatal(fmt.Sprintf("检查表结构失败: %v", err))
	}
	if !exists {
		fatal("库里没有 auth_users 表：确认 DSN 指向的是 d-auth 的库，并已执行迁移")
	}

	created, skipped, err := insertUsers(ctx, conn, users, *batch)
	if err != nil {
		fatal(fmt.Sprintf("写入失败（事务已回滚）: %v", err))
	}

	fmt.Printf("完成：新建 %d 个，跳过 %d 个（已存在）。\n", created, skipped)
	if skipped > 0 {
		fmt.Println("提示：跳过说明这些手机号已在库里，工具按手机号判重，重跑不会产生重复数据。")
	}
}

// resolveDSN 按 参数 > 环境变量 > .env 的顺序取 DSN。返回值和它的来源说明。
func resolveDSN(flagValue string) (dsn, source string, err error) {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v, "-dsn 参数", nil
	}
	if v := strings.TrimSpace(os.Getenv("DAUTH_POSTGRES_DSN")); v != "" {
		return v, "环境变量 DAUTH_POSTGRES_DSN", nil
	}
	path, err := findDotEnv()
	if err == nil {
		if v, ok := readEnvKey(path, "DAUTH_POSTGRES_DSN"); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v), path, nil
		}
	}
	return "", "", errors.New("找不到 DSN：用 -dsn 指定，或设置 DAUTH_POSTGRES_DSN，或在仓库根 .env 里配置")
}

// findDotEnv 从当前目录逐级向上找仓库根的 .env。
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
			return "", errors.New(".env not found")
		}
		dir = parent
	}
}

// readEnvKey 从 .env 里读一个键，支持 `KEY=value`、`KEY="value"`、`KEY='value'`，
// 忽略注释与空行。只做这一件事，不引入 dotenv 依赖。
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

// guardLocalOnly 拒绝把测试数据写进非本机库。
//
// 这个工具会写数据，而 DSN 可能指向任何地方；默认只放行本机，远程必须显式
// 加 -allow-remote。宁可多一步，也不要出现「以为是本地，其实写了线上」。
func guardLocalOnly(dsn string, allowRemote bool) error {
	host := hostOf(dsn)
	if isLocalHost(host) {
		return nil
	}
	if allowRemote {
		fmt.Printf("注意：已放行远程库 %s（-allow-remote）。\n\n", host)
		return nil
	}
	return fmt.Errorf("拒绝连接非本机数据库 %q：本工具会写数据，默认只允许 localhost/127.0.0.1/::1。\n"+
		"确认目标库没问题时，加 -allow-remote 显式放行", host)
}

func hostOf(dsn string) string {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return ""
	}
	return cfg.Host
}

func isLocalHost(host string) bool {
	switch host {
	case "", "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	// 以 / 开头的是 unix socket 目录，同样是本机
	return strings.HasPrefix(host, "/")
}

// describeDSN 输出不带密码的库位置，便于运行前一眼确认连的是哪个库。
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

// buildUsers 按批次生成用户列表。手机号 = 120 + 批次(2 位) + 序号(6 位)，
// 共 11 位纯数字：符合后台建号对虚拟号的宽松校验（AdminPhone 对 120 开头的
// 纯数字原样保留，不套 +86），同时保证不同批次、不同序号都互不撞号。
func buildUsers(batch int, spread, batchGap time.Duration) []seedUser {
	names := make([]string, 0, len(englishNames)+len(chineseNames))
	names = append(names, englishNames...)
	names = append(names, chineseNames...)

	now := time.Now()
	total := len(names)
	batchOffset := time.Duration(batch-1) * batchGap

	users := make([]seedUser, 0, total)
	for i, name := range names {
		nickname := name
		if batch > 1 {
			// 第 2 批及以后加后缀：昵称仍是可读的英文/中文名，但一眼能看出是第几批
			nickname = fmt.Sprintf("%s-%d", name, batch)
		}
		// 序号 1 起，让第 1 批是 12001xxxxxx 而不是 12001000000
		phone := fmt.Sprintf("120%02d%06d", batch, i+1)

		// 序号越小越早注册：Alice 最老，最后一位中文名最新，这样「按注册时间
		// 倒序」的默认列表能呈现真实分布，日期筛选也有可切分的数据。
		created := now.Add(-batchOffset - time.Duration(total-1-i)*spread)

		var lastSeen *time.Time
		if i%2 == 0 {
			// 偶数序号给一个「注册之后、现在之前」的登录时间，奇数序号保持从未登录，
			// 用来验证 last_login_at 的空值排序（NULLS LAST）。
			//
			// 取 created 到 now 的 2/3 处而不是 created+固定时长：固定加 48h 对刚
			// 注册的新用户会落到未来，等于全员「从未登录」，反而验证不出空值与非
			// 空值混排的效果。
			if gap := now.Sub(created); gap > 0 {
				t := created.Add(gap * 2 / 3)
				lastSeen = &t
			}
		}

		users = append(users, seedUser{
			Nickname: nickname,
			Phone:    phone,
			Created:  created,
			LastSeen: lastSeen,
		})
	}
	return users
}

// insertUsers 在一个事务里写完所有用户，任一失败全部回滚。
//
// 判重按手机号：已存在就跳过而不是报错，所以重跑同一批次是安全的、可重复的。
func insertUsers(ctx context.Context, conn *pgx.Conn, users []seedUser, batch int) (created, skipped int, err error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	for _, u := range users {
		var exists bool
		if err = tx.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM auth_identities WHERE type = 'phone' AND identifier = $1)`,
			u.Phone).Scan(&exists); err != nil {
			return created, skipped, err
		}
		if exists {
			skipped++
			continue
		}

		userID := newUserID()
		if _, err = tx.Exec(ctx,
			`INSERT INTO auth_users (id, nickname, status, last_login_at, created_at, updated_at)
			 VALUES ($1, $2, 'active', $3, $4, $4)`,
			userID, u.Nickname, u.LastSeen, u.Created); err != nil {
			return created, skipped, fmt.Errorf("%s: %w", u.Nickname, err)
		}

		// 身份直接标为已验证，并带上建号时间，和后台手工建号的结果一致。
		if _, err = tx.Exec(ctx,
			`INSERT INTO auth_identities (id, user_id, type, identifier, verified_at, created_at)
			 VALUES ($1, $2, 'phone', $3, $4, $4)`,
			newUUID(), userID, u.Phone, u.Created); err != nil {
			return created, skipped, fmt.Errorf("%s 身份: %w", u.Nickname, err)
		}

		// 角色取库里已有的 user 角色；角色表缺失时这里插入 0 行，不会失败。
		if _, err = tx.Exec(ctx,
			`INSERT INTO auth_user_roles (user_id, role_id)
			 SELECT $1, id FROM auth_roles WHERE code = 'user'
			 ON CONFLICT DO NOTHING`,
			userID); err != nil {
			return created, skipped, fmt.Errorf("%s 角色: %w", u.Nickname, err)
		}
		created++
	}

	if err = tx.Commit(ctx); err != nil {
		return created, skipped, err
	}
	return created, skipped, nil
}

// newUserID 生成 22 位小写字母数字串，与 api/internal/idgen.NewUserID 的结果形态一致。
// 用拒绝采样避免取模偏置——这里只是测试数据，但仍保持与真实 ID 相同的形状，
// 免得工具生成的用户触发了业务里针对 ID 格式的校验分支。
func newUserID() string {
	const maxAcceptable = 252 // 256 - (256 % 36)
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

// newUUID 生成一个随机 uuid（v4 形状）。只用于 auth_identities.id，
// 写测试数据不需要 v7 的时间有序性。
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant RFC 4122
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "错误: "+msg)
	os.Exit(1)
}