// Package idgen 负责生成与解析各类 ID。
//
// 用户 ID 是对外的友好标识：22 位小写字母数字（NanoID 风格）。库里 text 列存的
// 就是这个裸串，接口返回的也是同一个串——不存在「库里与对外不一致」。
// 其它实体（身份/会话/角色/客户端/API Key/审计）的 ID 仍是 uuid v7，由 New 生成。
package idgen

import (
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// userIDAlphabet 是用户 ID 的字符表：小写字母 + 数字。刻意不含大写，避免
// 大小写混淆；也不含 -/_ 等符号，保证在 URL、表格里都是干净的裸串。
const userIDAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// UserIDLen 是用户 ID 的长度。36^22 ≈ 1.3e34，随机空间足够大到无需碰撞担忧。
const UserIDLen = 22

// New 生成 uuid v7。按时间有序，字符串序与创建顺序一致。用于用户之外的实体。
func New() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Must(uuid.NewV7())
	}
	return id
}

func NewString() string {
	return New().String()
}

// NewUserID 生成一个新的用户 ID：22 位小写字母数字串。
//
// 用 crypto/rand 并做拒绝采样，避免「取模偏置」——直接对 256 取模会让字母表
// 前几个字符出现得略多。随机 ID 不可按时间排序，排序一律依赖 created_at。
func NewUserID() string {
	// 拒绝上界：256 - (256 % 36) = 252，只接受 < 252 的字节，从而每个字符
	// 等概率。剩下的 4 个值（252..255）丢弃重取。
	const maxAcceptable = 252
	out := make([]byte, UserIDLen)
	buf := make([]byte, UserIDLen)
	for i := 0; i < UserIDLen; {
		if _, err := rand.Read(buf); err != nil {
			// crypto/rand 在正常平台上不会失败；真失败也只能中止，而不是
			// 退化到弱随机——ID 是主键，弱随机意味着可预测、可碰撞。
			panic(fmt.Sprintf("idgen: crypto/rand failed: %v", err))
		}
		for _, b := range buf {
			if b >= maxAcceptable {
				continue
			}
			out[i] = userIDAlphabet[int(b)%len(userIDAlphabet)]
			i++
			if i == UserIDLen {
				break
			}
		}
	}
	return string(out)
}

// FormatUserID 把用户 ID 转成对外形式。
//
// 用户 ID 现在本身就是对外用的字符串，这个函数只做归一化（trim），保留它是因为
// 它是用户 ID 所有对外输出的唯一接缝——将来若要再调格式，只有这一处要动。
func FormatUserID(id string) string {
	return strings.TrimSpace(id)
}

// ParseUserID 解析对外传入的用户 ID：必须是 22 位、全在字母表内的小写字母数字串。
//
// 只接受新格式。旧的 uuid 字符串不再解析——这正是本次硬切的目的（见 0003 迁移）。
func ParseUserID(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) != UserIDLen {
		return "", fmt.Errorf("idgen: user id must be %d chars, got %d", UserIDLen, len(s))
	}
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune(userIDAlphabet, rune(s[i])) {
			return "", fmt.Errorf("idgen: user id has illegal char %q", s[i])
		}
	}
	return s, nil
}
