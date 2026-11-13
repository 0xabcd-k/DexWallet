package transfer

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestReceiveCodeRejectsTamperingAmountsAndForeignLinks(t *testing.T) {
	cfg := Config{DefaultLimitPlanID: 1, ReceiveSecret: strings.Repeat("s", 32), WebURL: "https://wallet.example/app/", TelegramURL: "https://t.me/example_bot/pay"}
	codes, err := NewCodes(cfg, "test")
	if err != nil {
		t.Fatal(err)
	}
	idx := "01234567890123456"
	code, err := codes.Encode(idx)
	if err != nil {
		t.Fatal(err)
	}
	web, bot := codes.Links(code)
	for _, raw := range []string{code, web, bot} {
		if got, err := codes.Resolve(raw); err != nil || got != idx {
			t.Fatal(raw, got, err)
		}
	}
	extra, _ := json.Marshal([]any{1, "account", idx, codes.mac(idx), "10.00"})
	wrongKind, _ := json.Marshal([]any{1, "merchant", idx, codes.mac(idx)})
	for _, raw := range []string{code + "=", code[:len(code)-2] + "xx", base64.RawURLEncoding.EncodeToString(extra), base64.RawURLEncoding.EncodeToString(wrongKind), web + "&amount=1", web + "&code=" + code, bot + "&startapp=" + code, strings.Replace(web, "wallet.example", "evil.example", 1), strings.Replace(bot, "example_bot", "other_bot", 1), strings.Replace(web, "/app/", "/different/", 1), strings.Repeat("a", 513)} {
		if _, err := codes.Resolve(raw); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	other, _ := NewCodes(cfg, "production")
	if _, err := other.Resolve(code); err == nil {
		t.Fatal("environment crossover")
	}
	cfg.ReceiveSecret = strings.Repeat("t", 32)
	rotated, _ := NewCodes(cfg, "test")
	if _, err := rotated.Resolve(code); err == nil {
		t.Fatal("old key accepted")
	}
}
func TestPublicTransferAmountsDoNotCoercePrecision(t *testing.T) {
	for _, raw := range []string{"0.01", "100", "100.00", "999999999999999999.99"} {
		if _, err := ParseAmount(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{"0", "-1", "01", "1.001", "1e2", " 1", "1.", ".01", "1000000000000000000"} {
		if _, err := ParseAmount(raw); err == nil {
			t.Fatal(raw)
		}
	}
}
