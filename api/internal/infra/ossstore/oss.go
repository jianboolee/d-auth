package ossstore

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"os"
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

type Mock struct {
	cfg     *config.Config
	root    string
	public  string
}

func NewMock(cfg *config.Config) *Mock {
	root := filepath.Join("data", "avatars")
	_ = os.MkdirAll(root, 0o755)
	return &Mock{cfg: cfg, root: root, public: cfg.OSSCDNBase}
}

func (m *Mock) SignPut(contentType, userID string) (string, string, string, map[string]string, error) {
	ext := extOf(contentType)
	key := fmt.Sprintf("%s/%s/%s%s", m.cfg.OSSPrefix, userID, idgen.NewString(), ext)
	upload := "/v1/me/avatar/upload?key=" + key
	return key, upload, m.PublicURL(key), map[string]string{"Content-Type": contentType}, nil
}

func (m *Mock) PublicURL(objectKey string) string {
	return strings.TrimRight(m.public, "/") + "/" + strings.TrimPrefix(objectKey, m.cfg.OSSPrefix+"/")
}

func (m *Mock) SaveLocal(objectKey string, data []byte) error {
	name := filepath.Base(objectKey)
	return os.WriteFile(filepath.Join(m.root, name), data, 0o644)
}

type Aliyun struct {
	cfg *config.Config
}

func NewAliyun(cfg *config.Config) *Aliyun { return &Aliyun{cfg: cfg} }

func (a *Aliyun) SignPut(contentType, userID string) (string, string, string, map[string]string, error) {
	ext := extOf(contentType)
	key := fmt.Sprintf("%s/%s/%s%s", a.cfg.OSSPrefix, userID, idgen.NewString(), ext)
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
	mac := hmac.New(sha1.New, []byte(a.cfg.AliyunAccessKeySecret))
	mac.Write([]byte(stringToSign))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	upload := fmt.Sprintf("%s/%s?OSSAccessKeyId=%s&Expires=%d&Signature=%s",
		host, key, a.cfg.AliyunAccessKeyID, expire, urlQuery(sig))
	return key, upload, a.PublicURL(key), map[string]string{"Content-Type": contentType}, nil
}

func (a *Aliyun) PublicURL(objectKey string) string {
	return strings.TrimRight(a.cfg.OSSCDNBase, "/") + "/" + strings.TrimPrefix(objectKey, a.cfg.OSSPrefix+"/")
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
