package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testBot = "123456:fake-bot-token-for-tests"

// Independent Telegram-documented HMAC formula, with no real bot credentials.
func signedTelegram(issued time.Time, user string) string {
	q := url.Values{"auth_date": {strconv.FormatInt(issued.Unix(), 10)}, "user": {user}, "query_id": {"query-1"}}
	var pairs []string
	for key := range q {
		pairs = append(pairs, key+"="+q.Get(key))
	}
	sort.Strings(pairs)
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(testBot))
	hash := hmac.New(sha256.New, secret.Sum(nil))
	hash.Write([]byte(strings.Join(pairs, "\n")))
	q.Set("hash", hex.EncodeToString(hash.Sum(nil)))
	return q.Encode()
}
func TestTelegramValidation(t *testing.T) {
	now := time.Unix(1800000000, 0)
	user := `{"id":9007199254740993,"first_name":"Alice"}`
	raw := signedTelegram(now, user)
	profile, hash, err := verifyTelegram(raw, testBot, now, false)
	if err != nil || profile.TelegramUserID != 9007199254740993 || profile.Username != nil || hash == "" {
		t.Fatalf("valid profile: %+v %s %v", profile, hash, err)
	}
	for name, input := range map[string]string{
		"empty": "", "duplicate": raw + "&auth_date=1", "forged": strings.Replace(raw, "Alice", "Other", 1),
		"old": signedTelegram(now.Add(-TelegramLifetime), user), "future": signedTelegram(now.Add(FutureTolerance+time.Second), user),
		"no user": signedTelegram(now, `{}`), "bot": signedTelegram(now, `{"id":1,"is_bot":true,"first_name":"bot"}`),
		"oversized": strings.Repeat("x", maxInitDataBytes+1), "newline": raw + "&extra=x%0Ay=z",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := verifyTelegram(input, testBot, now, false); err == nil {
				t.Fatal("invalid authorization accepted")
			}
		})
	}
	if _, _, err := verifyTelegram(raw, "wrong", now, false); err == nil {
		t.Fatal("wrong bot accepted")
	}
	parts := strings.Split(raw, "&")
	sort.Sort(sort.Reverse(sort.StringSlice(parts)))
	_, reordered, err := verifyTelegram(strings.Join(parts, "&"), testBot, now, false)
	if err != nil || reordered != hash {
		t.Fatal("proof digest depends on query order")
	}
}

func TestDevelopmentTelegramHasNoAgeLimitButStillVerifiesSignature(t *testing.T) {
	now := time.Unix(1800000000, 0)
	raw := signedTelegram(now.AddDate(-10, 0, 0), `{"id":7,"first_name":"Development"}`)
	if _, _, err := verifyTelegram(raw, testBot, now, false); err == nil {
		t.Fatal("ordinary authorization accepted expired material")
	}
	if _, _, err := verifyTelegram(raw, testBot, now, true); err != nil {
		t.Fatal("development material expired", err)
	}
	if _, _, err := verifyTelegram(raw, "wrong", now, true); err == nil {
		t.Fatal("development accepted wrong signature")
	}
}
