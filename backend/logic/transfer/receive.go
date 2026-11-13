package transfer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

const MaxCodeLength = 512

// DefaultPlanID is the migration seed; deployment configuration can select a
// different existing immutable plan for subsequent registrations.
const DefaultPlanID int64 = 1

type Config struct {
	DefaultLimitPlanID int64  `json:",default=1"`
	ReceiveSecret      string `json:",optional"`
	WebURL             string `json:",optional"`
	TelegramURL        string `json:",optional"`
}

func (c Config) Check() error {
	if c.DefaultLimitPlanID < 0 {
		return errors.New("default transfer plan ID must be positive")
	}
	if c.ReceiveSecret == "" && c.WebURL == "" && c.TelegramURL == "" {
		return nil
	}
	if len(c.ReceiveSecret) < 32 {
		return errors.New("independent receive-code secret of at least 32 bytes required")
	}
	web, err := url.Parse(c.WebURL)
	if err != nil || web.Host == "" || web.User != nil || web.RawQuery != "" || web.Fragment != "" || (web.Scheme != "https" && !(web.Scheme == "http" && (web.Hostname() == "localhost" || web.Hostname() == "127.0.0.1"))) {
		return errors.New("invalid receive-code WebURL")
	}
	if c.TelegramURL != "" {
		bot, err := url.Parse(c.TelegramURL)
		if err != nil || bot.Scheme != "https" || bot.Host != "t.me" || bot.User != nil || bot.RawQuery != "" || bot.Fragment != "" || strings.Trim(bot.Path, "/") == "" {
			return errors.New("invalid official Telegram app URL")
		}
	}
	return nil
}

type Codes struct {
	cfg         Config
	environment string
}

func NewCodes(c Config, environment string) (*Codes, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	return &Codes{c, environment}, nil
}
func (c *Codes) Available() bool { return c != nil && len(c.cfg.ReceiveSecret) >= 32 }
func (c *Codes) mac(idx string) string {
	payload, _ := json.Marshal([]any{1, "account", idx, c.environment})
	h := hmac.New(sha256.New, []byte(c.cfg.ReceiveSecret))
	h.Write(payload)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
func (c *Codes) Encode(idx string) (string, error) {
	if !c.Available() {
		return "", errors.New("receive codes not configured")
	}
	payload, _ := json.Marshal([]any{1, "account", idx, c.mac(idx)})
	return base64.RawURLEncoding.EncodeToString(payload), nil
}
func (c *Codes) Links(code string) (string, string) {
	web := c.cfg.WebURL + "#/pay?code=" + url.QueryEscape(code)
	bot := ""
	if c.cfg.TelegramURL != "" {
		bot = c.cfg.TelegramURL + "?startapp=" + url.QueryEscape(code)
	}
	return web, bot
}

// Resolve accepts only our exact configured share URLs or raw signed codes.
// No external resource is fetched; amount and duplicate/unknown fields reject.
func (c *Codes) Resolve(raw string) (string, error) {
	if !c.Available() || len(raw) > 2048 {
		return "", ErrInvalidRequest
	}
	code := raw
	if strings.Contains(raw, ":") {
		u, err := url.Parse(raw)
		if err != nil || u.User != nil {
			return "", ErrInvalidRequest
		}
		web, _ := url.Parse(c.cfg.WebURL)
		switch {
		case u.Scheme == web.Scheme && u.Host == web.Host && u.EscapedPath() == web.EscapedPath() && u.RawQuery == "":
			fragment, err := url.Parse(u.Fragment)
			if err != nil || fragment.Path != "/pay" || fragment.Fragment != "" {
				return "", ErrInvalidRequest
			}
			q, err := url.ParseQuery(fragment.RawQuery)
			if err != nil || len(q) != 1 || len(q["code"]) != 1 {
				return "", ErrInvalidRequest
			}
			code = q.Get("code")
		case c.cfg.TelegramURL != "" && u.Fragment == "":
			bot, _ := url.Parse(c.cfg.TelegramURL)
			if u.Scheme != bot.Scheme || u.Host != bot.Host || u.EscapedPath() != bot.EscapedPath() {
				return "", ErrInvalidRequest
			}
			q, err := url.ParseQuery(u.RawQuery)
			if err != nil || len(q) != 1 || len(q["startapp"]) != 1 {
				return "", ErrInvalidRequest
			}
			code = q.Get("startapp")
		default:
			return "", ErrInvalidRequest
		}
	}
	if len(code) == 0 || len(code) > MaxCodeLength {
		return "", ErrInvalidRequest
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(code)
	if err != nil || base64.RawURLEncoding.EncodeToString(b) != code {
		return "", ErrInvalidRequest
	}
	var values []json.RawMessage
	if err = json.Unmarshal(b, &values); err != nil || len(values) != 4 || string(values[0]) != "1" || string(values[1]) != `"account"` {
		return "", ErrInvalidRequest
	}
	var idx, mac string
	if json.Unmarshal(values[2], &idx) != nil || json.Unmarshal(values[3], &mac) != nil || !hmac.Equal([]byte(c.mac(idx)), []byte(mac)) {
		return "", ErrInvalidRequest
	}
	canonical, _ := c.Encode(idx)
	if canonical != code {
		return "", ErrInvalidRequest
	}
	return idx, nil
}
