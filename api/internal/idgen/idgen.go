// Package idgen 负责生成与解析各类 ID。
//
// 用户 ID 与其它实体一致，都是 uuid v7（按时间有序）。历史上有过一版 xid 风格
// 的实验：把 16 位 base36 串按「一个字符一个字节」塞进 uuid 列（每字节落在
// 0x30..0x53）。列类型当时就是 uuid，所以以十六进制加连字符渲染出来，看起来
// 像个 UUID，但并不是——对外接口则解包回 16 位串输出。
//
// 结果是同一个用户 ID 在数据库里和接口里长得完全不同，排查问题时对不上。现在
// 统一为 uuid：库里怎么渲染，对外就怎么给。
//
// 存量数据无需迁移：那些行存的仍是原样 16 个字节，只是显示方式变了。为了不让
// 老链接与老 JWT 失效，ParseUserID 仍然接受旧的 16 位打包串——只读兼容，新的
// ID 永远不会再产出这种形式。
package idgen

import (
	"strings"

	"github.com/google/uuid"
)

// 以下常量只用于解析历史的 16 位打包串（见 ParseUserID），生成路径已不再使用。
const (
	userIDLen      = 16
	userIDAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	userIDByteBase = 0x30
)

// New 生成 uuid v7。按时间有序，字符串序与创建顺序一致。
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

// NewUserID 生成一个新的用户 ID，即 uuid v7，与其它实体一致。
//
// 保留这个函数而不是让调用方直接用 New，是为了留下「用户 ID」这个语义标记：
// 曾经它和一个通用 UUID 不是一回事。
func NewUserID() uuid.UUID {
	return New()
}

// FormatUserID 把用户 ID 转成对外形式。
//
// 恒为 uuid 字符串（带连字符），与数据库 uuid 列的渲染一致——这正是这次改动的
// 目的：不再出现「库里一个样、界面另一个样」。
//
// 保留这个函数而不是让调用方直接用 u.String()：它是所有对外输出的唯一接缝，
// 命名本身就说明了这里是用户 ID 的 API 边界，将来若要再调整对外格式只会有一处。
func FormatUserID(u uuid.UUID) string {
	return u.String()
}

// ParseUserID 解析对外传入的用户 ID。
//
// 接受两种形式：标准 uuid 字符串，以及历史的 16 位 base36 打包串。
// 后者是只读兼容——库里存量行仍然存着那种字节，老书签和老 JWT 的 sub 因此不会
// 失效；但生成侧已经不再产出这种形式。
func ParseUserID(s string) (uuid.UUID, error) {
	s = strings.TrimSpace(s)
	if u, ok := packUserID(s); ok {
		return u, nil
	}
	return uuid.Parse(s)
}

// packUserID 把历史的 16 位 base36 串还原成它所在的 16 个字节
// （每字节 = 0x30 + 该字符在字母表里的下标）。仅用于读取存量数据。
func packUserID(s string) (uuid.UUID, bool) {
	if len(s) != userIDLen {
		return uuid.Nil, false
	}
	var u uuid.UUID
	for i := 0; i < userIDLen; i++ {
		idx := strings.IndexByte(userIDAlphabet, s[i])
		if idx < 0 {
			return uuid.Nil, false
		}
		u[i] = byte(userIDByteBase + idx)
	}
	return u, true
}
