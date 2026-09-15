// Package idgen 负责生成与解析各类 ID。
//
// 所有实体的 ID 都是 uuid v7（按时间有序，字符串序与创建顺序一致）。用户 ID
// 也不例外：库里 uuid 列怎么渲染，对外就怎么给，两端看到的是同一个字符串。
package idgen

import (
	"strings"

	"github.com/google/uuid"
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

// NewUserID 生成一个新的用户 ID。
//
// 与 New 相同，保留这个函数只为留下「用户 ID」这个语义标记。
func NewUserID() uuid.UUID {
	return New()
}

// FormatUserID 把用户 ID 转成对外形式，即 uuid 字符串。
//
// 保留这个函数而不是让调用方直接用 u.String()：它是用户 ID 所有对外输出的
// 唯一接缝，命名本身说明了这里是 API 边界，将来若要调整对外格式只有一处。
func FormatUserID(u uuid.UUID) string {
	return u.String()
}

// ParseUserID 解析对外传入的用户 ID，即 uuid 字符串。
func ParseUserID(s string) (uuid.UUID, error) {
	return uuid.Parse(strings.TrimSpace(s))
}
