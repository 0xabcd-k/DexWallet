package user

import (
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"
)

type EmailAddress struct{ Address, LocalPart, Domain, Original string }

// NormalizeEmail preserves the local part exactly; it does not fold provider aliases.
// This is storage normalization, not proof of mailbox ownership.
func NormalizeEmail(original string) (EmailAddress, error) {
	s := strings.TrimSpace(original)
	parsed, err := mail.ParseAddress(s)
	at := strings.LastIndexByte(s, '@')
	if err != nil || parsed.Address != s || at <= 0 || at == len(s)-1 || !validText(original, 512) || len(s) > 512 {
		return EmailAddress{}, fmt.Errorf("%w: email", ErrInvalidInput)
	}
	local, domain := s[:at], strings.ToLower(s[at+1:])
	if len(local) > 512 || !validText(domain, 255) || len(local)+1+len(domain) > 512 {
		return EmailAddress{}, fmt.Errorf("%w: email capacity", ErrInvalidInput)
	}
	return EmailAddress{local + "@" + domain, local, domain, original}, nil
}

// VerifiedTelegram is a complete, server-verified profile snapshot, not a patch.
// The caller in authentication must verify provenance/freshness before calling.
type VerifiedTelegram struct {
	TelegramUserID                             int64
	FirstName                                  string
	Username, LastName, PhotoURL, LanguageCode *string
}

func (p VerifiedTelegram) validate() error {
	if p.TelegramUserID <= 0 || p.FirstName == "" || !validText(p.FirstName, 256) {
		return fmt.Errorf("%w: telegram id/first name", ErrInvalidInput)
	}
	for _, field := range []struct {
		value *string
		max   int
	}{
		{p.Username, 128}, {p.LastName, 256}, {p.PhotoURL, 2048}, {p.LanguageCode, 64},
	} {
		if field.value != nil && !validText(*field.value, field.max) {
			return fmt.Errorf("%w: telegram profile", ErrInvalidInput)
		}
	}
	return nil
}
func validText(value string, max int) bool {
	return utf8.ValidString(value) && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= max
}
