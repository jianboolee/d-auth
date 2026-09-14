package idgen

import (
	"crypto/rand"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// 用户 ID 采用 xid 风格：16 位、仅数字与小写字母、按时间有序。
// 组织方式为「8 位秒级时间戳 + 8 位进程内序号」：固定宽度的 base36 时间前缀
// 让整体按时间递增，同一秒内再由进程内单调递增的序号保证唯一。字符集为
// base36（0-9a-z），因此字符串字典序与生成顺序一致（跨进程不保证同秒内有序）。
const (
	userIDLen      = 16
	userIDTimeLen  = 8
	userIDAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
)

// 用户 ID 以「一个字符一个字节」的方式放进 uuid.UUID 的 16 字节里：字符按其在
// base36 字母表里的下标映射到 0x30..0x53。这样每个字节都落在这个区间内（高四位
// 只会是 3/4/5）。而任何标准 UUID 的第 9 个字节（下标 8）都带 RFC 4122 variant
// 位，恒为 0x80..0xbf（高两位为 10），一定超出该区间。所以「16 个字节全落在
// 0x30..0x53」可以无歧义地把打包的用户 ID 与历史 uuid v7 用户 ID 区分开
// （见 isPackedUserID），无需依赖版本号。
const userIDByteBase = 0x30

var (
	userIDSeq atomic.Uint32
	userIDTag = randomTag()
)

// New 为会话、身份、客户端、API Key 等实体生成 UUID v7。
// 用户 ID 请使用 NewUserID。
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

// NewUserID 生成一个新的用户 ID。返回值仍是 uuid.UUID（列类型不变），
// 但其中打包的是 16 位小写字母数字串；对外用 FormatUserID 还原成 16 位字符串。
func NewUserID() uuid.UUID {
	u, _ := packUserID(newUserIDString())
	return u
}

// FormatUserID 把用户 ID 转成对外形式：新用户为 16 位小写字母数字串，
// 历史 uuid v7 用户保持 36 位 UUID 字符串，无需迁移。
func FormatUserID(u uuid.UUID) string {
	if isPackedUserID(u) {
		return unpackUserID(u)
	}
	return u.String()
}

// ParseUserID 解析对外传入的用户 ID，同时兼容 16 位新串与历史 UUID 字符串。
func ParseUserID(s string) (uuid.UUID, error) {
	s = strings.TrimSpace(s)
	if u, ok := packUserID(s); ok {
		return u, nil
	}
	return uuid.Parse(s)
}

func newUserIDString() string {
	sec := uint64(time.Now().Unix())
	// 8 位 base36 可表达 36^8-1 ≈ 2.8e12 秒，约 8.9 万年，不会用完。
	// 低 41 位：24 位进程随机标记（重启后换一批，降低跨进程碰撞）+ 17 位进程内序号。
	seed := (uint64(userIDTag)&0xFFFFFF)<<17 | uint64(userIDSeq.Add(1)&0x1FFFF)
	return encodeBase36(sec, userIDTimeLen) + encodeBase36(seed, userIDLen-userIDTimeLen)
}

func encodeBase36(v uint64, width int) string {
	buf := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		buf[i] = userIDAlphabet[v%36]
		v /= 36
	}
	return string(buf)
}

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

func unpackUserID(u uuid.UUID) string {
	buf := make([]byte, userIDLen)
	for i := 0; i < userIDLen; i++ {
		buf[i] = userIDAlphabet[u[i]-userIDByteBase]
	}
	return string(buf)
}

func isPackedUserID(u uuid.UUID) bool {
	for _, b := range u {
		if b < userIDByteBase || b > userIDByteBase+byte(len(userIDAlphabet)-1) {
			return false
		}
	}
	return true
}

func randomTag() uint32 {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint32(time.Now().UnixNano()) & 0xFFFFFF
	}
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
}
