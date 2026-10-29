package auth

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"strings"
	"text/template"
	"time"

	mail "github.com/wneessen/go-mail"
)

//go:embed templates/*.txt
var mailTemplates embed.FS

type SMTPConfig struct {
	Host        string        `json:",optional"`
	Port        int           `json:",default=587"`
	From        string        `json:",optional"`
	Username    string        `json:",optional"`
	Password    string        `json:",optional"`
	ImplicitTLS bool          `json:",default=false"`
	Timeout     time.Duration `json:",default=10s"`
}
type SMTPMailer struct{ cfg SMTPConfig }

func NewSMTPMailer(cfg SMTPConfig) (*SMTPMailer, error) {
	if cfg.Host == "" || cfg.Port < 1 || cfg.Port > 65535 || cfg.Timeout <= 0 || cfg.Timeout >= ResendInterval || cfg.From == "" || (cfg.Username == "") != (cfg.Password == "") {
		return nil, errors.New("invalid SMTP configuration")
	}
	m := mail.NewMsg()
	if err := m.From(cfg.From); err != nil {
		return nil, err
	}
	return &SMTPMailer{cfg}, nil
}
func message(from, to, code, locale string) (*mail.Msg, error) {
	if locale != "zh-CN" {
		locale = "en"
	}
	text, err := mailTemplates.ReadFile("templates/" + locale + ".txt")
	if err != nil {
		return nil, err
	}
	subject, body, _ := strings.Cut(string(text), "\n")
	tmpl, err := template.New("code").Parse(body)
	if err != nil {
		return nil, err
	}
	var rendered bytes.Buffer
	if err = tmpl.Execute(&rendered, struct {
		Code    string
		Minutes int
	}{code, int(CodeLifetime / time.Minute)}); err != nil {
		return nil, err
	}
	m := mail.NewMsg()
	if err = m.From(from); err != nil {
		return nil, err
	}
	if err = m.To(to); err != nil {
		return nil, err
	}
	m.Subject(subject)
	m.SetBodyString(mail.TypeTextPlain, rendered.String())
	return m, nil
}
func (m *SMTPMailer) SendCode(ctx context.Context, to, code, locale string) error {
	msg, err := message(m.cfg.From, to, code, locale)
	if err != nil {
		return err
	}
	options := []mail.Option{mail.WithPort(m.cfg.Port), mail.WithTimeout(m.cfg.Timeout), mail.WithTLSPolicy(mail.TLSMandatory)}
	if m.cfg.ImplicitTLS {
		options = append(options, mail.WithSSL())
	}
	if m.cfg.Username != "" {
		options = append(options, mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover), mail.WithUsername(m.cfg.Username), mail.WithPassword(m.cfg.Password))
	}
	client, err := mail.NewClient(m.cfg.Host, options...)
	if err != nil {
		return err
	}
	call, cancel := context.WithTimeout(ctx, m.cfg.Timeout)
	defer cancel()
	return client.DialAndSendWithContext(call, msg)
}
