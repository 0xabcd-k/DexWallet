package auth

import (
	"errors"
	"time"
)

const (
	CodeLifetime      = 5 * time.Minute
	ResendInterval    = time.Minute
	MaxCodeAttempts   = int64(5)
	TelegramLifetime  = 5 * time.Minute
	FutureTolerance   = 30 * time.Second
	OperationLifetime = 5 * time.Minute
	CodeSpace         = int64(1000000)
	resolveAttempts   = 3
)

type Config struct {
	EmailEnabled        bool       `json:",default=false"`
	TelegramEnabled     bool       `json:",default=false"`
	TelegramDevelopment bool       `json:",default=false"`
	Secret              string     `json:",optional"`
	BotToken            string     `json:",optional"`
	SendPerEmail        int        `json:",default=10"`
	SendPerSource       int        `json:",default=30"`
	VerifyPerSubject    int        `json:",default=30"`
	VerifyPerSource     int        `json:",default=60"`
	SMTP                SMTPConfig `json:",optional"`
}

func (c Config) Check() error {
	if len(c.Secret) < 32 {
		return errors.New("auth secret must contain at least 32 bytes")
	}
	if c.SendPerEmail < 1 || c.SendPerSource < 1 || c.VerifyPerSubject < 1 || c.VerifyPerSource < 1 {
		return errors.New("auth rate limits must be positive")
	}
	if c.TelegramEnabled && c.BotToken == "" {
		return errors.New("Telegram bot token required")
	}
	return nil
}
func DefaultConfig() Config {
	return Config{SendPerEmail: 10, SendPerSource: 30, VerifyPerSubject: 30, VerifyPerSource: 60}
}
