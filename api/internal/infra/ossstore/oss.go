package ossstore

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/idgen"
)

type Store interface {
	SignPut(contentType string, userID string) (objectKey, uploadURL, publicURL string, headers map[string]string, err error)
	PublicURL(objectKey string) string
	SaveLocal(objectKey string, data []byte) error
}

// DataRoot 是 mock 模式在本地磁盘上的存储基目录。对象键（含 OSSRootDir）直接拼在其下，
// 即 data/<rootDir>/<userID>/<name>；静态服务与 SaveLocal 都以它为基准，避免重复前缀。
func DataRoot() string { return "data" }

func objectKey(rootDir, userID, ext string) string {
	return fmt.Sprintf("%s/%s/%s%s", rootDir, userID, idgen.NewString(), ext)
}

type Mock struct {
	cfg *config.Config
}

func NewMock(cfg *config.Config) *Mock {
	_ = os.MkdirAll(filepath.Join(DataRoot(), cfg.OSSRootDir), 0o755)
	return &Mock{cfg: cfg}
}

func (m *Mock) SignPut(contentType, userID string) (string, string, string, map[string]string, error) {
	key := objectKey(m.cfg.OSSRootDir, userID, extOf(contentType))
	upload := "/v1/me/avatar/upload?key=" + key
	return key, upload, m.PublicURL(key), map[string]string{"Content-Type": contentType}, nil
}

// PublicURL 统一走 config.AvatarURL，保证与用户信息里的 avatar_url 完全一致。
func (m *Mock) PublicURL(objectKey string) string {
	return m.cfg.AvatarURL(objectKey)
}

func (m *Mock) SaveLocal(objectKey string, data []byte) error {
	clean := path.Clean(strings.TrimPrefix(strings.TrimSpace(objectKey), "/"))
	if clean == "." || !strings.HasPrefix(clean, m.cfg.OSSRootDir+"/") {
		return fmt.Errorf("invalid object key: %q", objectKey)
	}
	full := filepath.Join(DataRoot(), filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}

type Aliyun struct {
	cfg *config.Config
}

func NewAliyun(cfg *config.Config) *Aliyun { return &Aliyun{cfg: cfg} }

func (a *Aliyun) SignPut(contentType, userID string) (string, string, string, map[string]string, error) {
	key := objectKey(a.cfg.OSSRootDir, userID, extOf(contentType))
	expire := time.Now().UTC().Add(10 * time.Minute).Unix()
	host := fmt.Sprintf("https://%s.%s", a.cfg.OSSBucket, strings.TrimPrefix(a.cfg.OSSEndpoint, "https://"))
	host = strings.TrimPrefix(host, "https://"+a.cfg.OSSBucket+".https://")
	if !strings.HasPrefix(a.cfg.OSSEndpoint, "http") {
		host = fmt.Sprintf("https://%s.%s", a.cfg.OSSBucket, a.cfg.OSSEndpoint)
	} else {
		ep := strings.TrimPrefix(strings.TrimPrefix(a.cfg.OSSEndpoint, "https://"), "http://")
		host = fmt.Sprintf("https://%s.%s", a.cfg.OSSBucket, ep)
	}
	resource := fmt.Sprintf("/%s/%s", a.cfg.OSSBucket, key)
	stringToSign := fmt.Sprintf("PUT\n\n%s\n%d\n%s", contentType, expire, resource)
	mac := hmac.New(sha1.New, []byte(a.cfg.OSSAccessKeySecret))
	mac.Write([]byte(stringToSign))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	upload := fmt.Sprintf("%s/%s?OSSAccessKeyId=%s&Expires=%d&Signature=%s",
		host, key, a.cfg.OSSAccessKeyID, expire, urlQuery(sig))
	return key, upload, a.PublicURL(key), map[string]string{"Content-Type": contentType}, nil
}

func (a *Aliyun) PublicURL(objectKey string) string {
	return a.cfg.AvatarURL(objectKey)
}

func (a *Aliyun) SaveLocal(string, []byte) error { return fmt.Errorf("aliyun oss does not save local") }

func extOf(ct string) string {
	switch strings.ToLower(ct) {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

func urlQuery(s string) string {
	r := strings.NewReplacer("+", "%2B", "/", "%2F", "=", "%3D")
	return r.Replace(s)
}
