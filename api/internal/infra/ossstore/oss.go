package ossstore

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
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
	Save(objectKey string, data []byte) error
}

// DataRoot 是 mock 模式在本地磁盘上的存储基目录。对象键（含 OSSRootDir）直接拼在其下，
// 即 data/<rootDir>/<userID>/<name>；静态服务与 mock 的 Save 都以它为基准，避免重复前缀。
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

func (m *Mock) Save(objectKey string, data []byte) error {
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
	cfg  *config.Config
	http *http.Client
}

func NewAliyun(cfg *config.Config) *Aliyun {
	return &Aliyun{
		cfg:  cfg,
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

// WithTransport 注入自定义 http 传输层，供测试替换网络；生产代码不调用。
func (a *Aliyun) WithTransport(rt http.RoundTripper) *Aliyun {
	a.http.Transport = rt
	return a
}

// SignPut 不再签发 OSS 直传地址（浏览器直传依赖 bucket CORS 配置，且签名 URL
// 暴露到公网容易被拦出 403），统一返回本服务的同源中转地址：
// 前端 PUT 到 /v1/me/avatar/upload，由服务端用 Header 签名写入 OSS。
func (a *Aliyun) SignPut(contentType, userID string) (string, string, string, map[string]string, error) {
	key := objectKey(a.cfg.OSSRootDir, userID, extOf(contentType))
	upload := "/v1/me/avatar/upload?key=" + key
	return key, upload, a.PublicURL(key), map[string]string{"Content-Type": contentType}, nil
}

func (a *Aliyun) PublicURL(objectKey string) string {
	return a.cfg.AvatarURL(objectKey)
}

// virtualHost 返回 bucket 的虚机机域名，如 https://wfc-01.oss-cn-beijing.aliyuncs.com。
func (a *Aliyun) virtualHost() string {
	ep := a.cfg.OSSEndpoint
	if !strings.HasPrefix(ep, "http") {
		return fmt.Sprintf("https://%s.%s", a.cfg.OSSBucket, ep)
	}
	ep = strings.TrimPrefix(strings.TrimPrefix(ep, "https://"), "http://")
	return fmt.Sprintf("https://%s.%s", a.cfg.OSSBucket, ep)
}

// Save 用 OSS V1 Header 签名（PUT + Date + Authorization）把字节写入 bucket。
func (a *Aliyun) Save(objectKey string, data []byte) error {
	clean := path.Clean(strings.TrimPrefix(strings.TrimSpace(objectKey), "/"))
	if clean == "." || !strings.HasPrefix(clean, a.cfg.OSSRootDir+"/") {
		return fmt.Errorf("invalid object key: %q", objectKey)
	}
	contentType := contentTypeForKey(clean)
	date := time.Now().UTC().Format(http.TimeFormat)
	resource := fmt.Sprintf("/%s/%s", a.cfg.OSSBucket, clean)
	stringToSign := fmt.Sprintf("PUT\n\n%s\n%s\n%s", contentType, date, resource)
	sig := a.sign(stringToSign)

	url := a.virtualHost() + "/" + clean
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Date", date)
	req.Header.Set("Authorization", fmt.Sprintf("OSS %s:%s", a.cfg.OSSAccessKeyID, sig))
	resp, err := a.http.Do(req)
	if err != nil {
		// 出网失败（DNS/防火墙）时给出一条能直接定位原因的包装错误。
		if strings.Contains(err.Error(), "no such host") {
			return fmt.Errorf("oss put: endpoint dns failed for %s (服务器无法解析 %s): %w", a.virtualHost(), a.cfg.OSSEndpoint, err)
		}
		return fmt.Errorf("oss put: network error to %s: %w", a.virtualHost(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("oss put: status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (a *Aliyun) sign(stringToSign string) string {
	mac := hmac.New(sha1.New, []byte(a.cfg.OSSAccessKeySecret))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// contentTypeForKey 按对象键扩展名反推 Content-Type，与 extOf 保持一致，
// PutObject 的签名必须带上与请求头完全相同的 Content-Type。
func contentTypeForKey(key string) string {
	switch strings.ToLower(path.Ext(key)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

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
