// Package legal 维护站点级法务文档（用户协议 / 隐私政策）。
//
// 单独成模块而不是放进 admin：读取端是公开接口，不应该依赖管理模块；
// 写入端由 admin 路由的超管校验守卫，这里只关心数据与校验本身。
package legal

import (
	"strings"
	"time"

	"github.com/jianboolee/d-auth/internal/apperr"
	"github.com/jianboolee/d-auth/internal/models"
	"github.com/jianboolee/d-auth/internal/modules/store"
)

// MaxContentBytes 是正文长度上限。协议正文可能很长，但不应无界——
// 防止一次 PUT 写入超大 blob 拖垮读写与前端渲染。
const MaxContentBytes = 100 * 1024

// MaxURLBytes 是链接长度上限，正常 URL 远小于此。
const MaxURLBytes = 2048

type Service struct {
	store *store.Store
}

func New(st *store.Store) *Service {
	return &Service{store: st}
}

// All 返回全部文档，按 kind 索引，便于前端一次取完。
// 表里预置了 terms/privacy 两行，但若被外部删掉，这里补齐为空文档，
// 避免前端因缺 key 而报错。
func (s *Service) All() (map[string]models.LegalDoc, error) {
	var list []models.LegalDoc
	if err := s.store.DB.Order("kind").Find(&list).Error; err != nil {
		return nil, err
	}
	out := make(map[string]models.LegalDoc, len(models.LegalKinds))
	for _, k := range models.LegalKinds {
		out[k] = models.LegalDoc{Kind: k}
	}
	for _, d := range list {
		out[d.Kind] = d
	}
	return out, nil
}

func (s *Service) Get(kind string) (models.LegalDoc, error) {
	if !models.IsLegalKind(kind) {
		return models.LegalDoc{}, errInvalidKind()
	}
	var doc models.LegalDoc
	if err := s.store.DB.Where("kind = ?", kind).First(&doc).Error; err != nil {
		return models.LegalDoc{}, err
	}
	return doc, nil
}

// Upsert 写入某个 kind 的 url 与 content。两者都传空即回到「未设置」状态。
func (s *Service) Upsert(kind, url, content string) (models.LegalDoc, error) {
	if !models.IsLegalKind(kind) {
		return models.LegalDoc{}, errInvalidKind()
	}
	url = strings.TrimSpace(url)
	content = strings.TrimSpace(content)
	if err := validateURL(url); err != nil {
		return models.LegalDoc{}, err
	}
	if len(url) > MaxURLBytes {
		return models.LegalDoc{}, apperr.New(apperr.ErrInvalidArgument, "URL_TOO_LONG", "链接过长")
	}
	if len(content) > MaxContentBytes {
		return models.LegalDoc{}, apperr.New(apperr.ErrInvalidArgument, "CONTENT_TOO_LONG", "正文过长（上限 100KB）")
	}

	doc := models.LegalDoc{Kind: kind, URL: url, Content: content, UpdatedAt: time.Now()}
	// 用 upsert 而不是先查再写：预置行存在时走更新，缺失时（被外部删掉）自动补回。
	if err := s.store.DB.Save(&doc).Error; err != nil {
		return models.LegalDoc{}, err
	}
	return doc, nil
}

// validateURL 只放行空值或 http(s) 绝对地址。
//
// 这条校验是安全边界，不只是格式校验：前端会把这个值交给 window.location.replace()，
// 若允许 javascript: 之类的伪协议，就等于把 XSS 存进了所有访问者都会加载的页面。
// 前端也有一道同样的检查（纵深防御），但服务端这道是权威。
func validateURL(raw string) error {
	if raw == "" {
		return nil
	}
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return apperr.New(apperr.ErrInvalidArgument, "INVALID_URL", "链接必须以 http:// 或 https:// 开头")
	}
	return nil
}

func errInvalidKind() error {
	return apperr.New(apperr.ErrInvalidArgument, "INVALID_KIND", "不支持的文档类型")
}
