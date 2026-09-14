package sms

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/infra/aliyunrpc"
)

type Aliyun struct {
	cfg *config.Config
}

func NewAliyun(cfg *config.Config) *Aliyun {
	return &Aliyun{cfg: cfg}
}

func (a *Aliyun) Send(ctx context.Context, phone, code, scene string) error {
	tpl := a.cfg.SMSTemplateLogin
	switch scene {
	case "bind_phone", "change_phone":
		if a.cfg.SMSTemplateBind != "" {
			tpl = a.cfg.SMSTemplateBind
		}
	case "reset_password":
		if a.cfg.SMSTemplateReset != "" {
			tpl = a.cfg.SMSTemplateReset
		}
	}
	phone = strings.TrimPrefix(phone, "+")
	params := map[string]string{
		"PhoneNumbers":  phone,
		"SignName":      a.cfg.SMSSignName,
		"TemplateCode":  tpl,
		"TemplateParam": templateParam(code, a.cfg.CodeTTL),
	}
	return aliyunrpc.Call(ctx, "dysmsapi.aliyuncs.com", "SendSms", "2017-05-25", a.cfg.AliyunSMSAccessKeyID, a.cfg.AliyunSMSAccessKeySecret, params)
}

// templateParam 生成短信模板参数。模板同时使用 ${code} 与 ${time}（X 分钟内有效），
// 必须两者都提供，否则阿里云会以 isv.SMS_TEMPLATE_ILLEGAL 拒绝发送。
func templateParam(code string, ttl time.Duration) string {
	minutes := int(ttl.Minutes())
	if minutes <= 0 {
		minutes = 5
	}
	return fmt.Sprintf(`{"code":"%s","time":"%d"}`, code, minutes)
}
