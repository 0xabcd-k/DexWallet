package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMailTemplatesAndFailure(t *testing.T) {
	for _, locale := range []string{"en", "zh-CN", "unknown"} {
		m, err := message("auth@example.com", "a@example.com", "012345", locale)
		if err != nil {
			t.Fatal(err)
		}
		var wire strings.Builder
		if _, err = m.WriteTo(&wire); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(wire.String(), "012345") {
			t.Fatal("code not present")
		}
	}
	if _, err := NewSMTPMailer(SMTPConfig{}); err == nil {
		t.Fatal("bad config accepted")
	}
	if _, err := message("auth@example.com", "a@example.com\r\nBcc: b@example.com", "012345", "en"); err == nil {
		t.Fatal("header injection accepted")
	}
	sender, err := NewSMTPMailer(SMTPConfig{Host: "127.0.0.1", Port: 1, From: "auth@example.com", Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = sender.SendCode(ctx, "a@example.com", "012345", "en"); err == nil {
		t.Fatal("cancelled delivery succeeded")
	}
}
