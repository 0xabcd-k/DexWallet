package user

import (
	"errors"
	"strings"
	"testing"
)

func TestEmailNormalization(t *testing.T) {
	got, err := NormalizeEmail(" Alice.Tag+One@EXAMPLE.COM ")
	if err != nil || got.Address != "Alice.Tag+One@example.com" || got.LocalPart != "Alice.Tag+One" || got.Domain != "example.com" || got.Original != " Alice.Tag+One@EXAMPLE.COM " {
		t.Fatalf("normalization: %+v %v", got, err)
	}
	for _, input := range []string{"", "alice", "a@@example.com", "Alice <a@example.com>", "a@example.com\x00", strings.Repeat("a", 513) + "@example.com", "a@" + strings.Repeat("d", 256)} {
		if _, err := NormalizeEmail(input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("accepted %q: %v", input, err)
		}
	}
	a, _ := NormalizeEmail("Alice@example.com")
	b, _ := NormalizeEmail("alice@example.com")
	if a.Address == b.Address {
		t.Fatal("local part folded")
	}
}
func TestTelegramInput(t *testing.T) {
	profile := VerifiedTelegram{TelegramUserID: 7, FirstName: "名字"}
	if err := profile.validate(); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("界", 129)
	profile.Username = &long
	if !errors.Is(profile.validate(), ErrInvalidInput) {
		t.Fatal("overlong profile")
	}
	profile.Username = nil
	profile.TelegramUserID = 0
	if !errors.Is(profile.validate(), ErrInvalidInput) {
		t.Fatal("invalid id")
	}
	if len([]rune(initialNickname(strings.Repeat("界", 256)))) != 128 {
		t.Fatal("initial nickname capacity")
	}
}
