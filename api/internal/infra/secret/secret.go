package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

func deriveKey(master string) []byte {
	sum := sha256.Sum256([]byte(master))
	return sum[:]
}

func Encrypt(master, plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(deriveKey(master))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawStdEncoding.EncodeToString(out), nil
}

func Decrypt(master, encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(deriveKey(master))
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func RandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// alnumAlphabet 是 API Key 随机体的字符表：大小写字母 + 数字。刻意不含 -/_ 等
// 符号，这样密钥里除固定前缀 sk- 外不会再有连字符，抄写与目视核对都不会混淆。
const alnumAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// RandomAlnum 返回 n 位字母数字串（A-Za-z0-9），用于 API Key 的随机体。
//
// 用 crypto/rand 并做拒绝采样：直接对 256 取模会让字母表前几个字符出现得略多，
// 密钥是不可预测的凭据，偏置意味着熵的浪费，因此丢弃掉尾部不完整的区间。
func RandomAlnum(n int) (string, error) {
	// 拒绝上界：256 - (256 % 62) = 248，只接受 < 248 的字节，从而每个字符等概率。
	const maxAcceptable = 256 - (256 % len(alnumAlphabet))
	out := make([]byte, n)
	buf := make([]byte, n)
	for i := 0; i < n; {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for _, b := range buf {
			if int(b) >= maxAcceptable {
				continue
			}
			out[i] = alnumAlphabet[int(b)%len(alnumAlphabet)]
			i++
			if i == n {
				break
			}
		}
	}
	return string(out), nil
}
