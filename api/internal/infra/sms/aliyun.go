package sms

import (
	"context"
	"fmt"
	"strings"

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
		"TemplateParam": fmt.Sprintf(`{"code":"%s"}`, code),
	}
	return aliyunrpc.Call(ctx, "dysmsapi.aliyuncs.com", "SendSms", "2017-05-25", a.cfg.AliyunAccessKeyID, a.cfg.AliyunAccessKeySecret, params)
}
