package mailer

import (
	"context"
	"fmt"
	"html"

	"github.com/jianboolee/d-auth/internal/config"
	"github.com/jianboolee/d-auth/internal/infra/aliyunrpc"
)

type Aliyun struct {
	cfg *config.Config
}

func NewAliyun(cfg *config.Config) *Aliyun {
	return &Aliyun{cfg: cfg}
}

func (a *Aliyun) SendCode(ctx context.Context, email, code, scene string) error {
	subject := "验证码"
	switch scene {
	case "register":
		subject = "注册验证码"
	case "reset_password":
		subject = "重置密码验证码"
	case "bind_email":
		subject = "绑定邮箱验证码"
	}
	minutes := int(a.cfg.CodeTTL.Minutes())
	if minutes <= 0 {
		minutes = 5
	}
	htmlBody := fmt.Sprintf(`<p>您的验证码是 <b>%s</b>，%d 分钟内有效。如非本人操作请忽略。</p>`, html.EscapeString(code), minutes)
	params := map[string]string{
		"AccountName":    a.cfg.DMAccount,
		"FromAlias":      a.cfg.DMFromAlias,
		"AddressType":    "1",
		"ToAddress":      email,
		"Subject":        subject,
		"HtmlBody":       htmlBody,
		"ReplyToAddress": "false",
	}
	host := "dm.aliyuncs.com"
	if a.cfg.DMRegion != "" && a.cfg.DMRegion != "cn-hangzhou" {
		host = fmt.Sprintf("dm.%s.aliyuncs.com", a.cfg.DMRegion)
	}
	return aliyunrpc.Call(ctx, host, "SingleSendMail", "2015-11-23", a.cfg.AliyunDMAccessKeyID, a.cfg.AliyunDMAccessKeySecret, params)
}
