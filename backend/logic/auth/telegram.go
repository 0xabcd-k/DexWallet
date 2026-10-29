package auth

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	initdata "github.com/telegram-mini-apps/init-data-golang"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

const maxInitDataBytes = 16384

func verifyTelegram(raw, bot string, now time.Time) (userlogic.VerifiedTelegram, string, error) {
	var empty userlogic.VerifiedTelegram
	if len(raw) == 0 || len(raw) > maxInitDataBytes || bot == "" {
		return empty, "", ErrTelegram
	}
	query, err := url.ParseQuery(raw)
	if err != nil || len(query) > 32 {
		return empty, "", ErrTelegram
	}
	for key, values := range query {
		if key == "" || len(values) != 1 || strings.ContainsAny(key, "\r\n") || strings.ContainsAny(values[0], "\r\n") {
			return empty, "", ErrTelegram
		}
	}
	seconds, err := strconv.ParseInt(query.Get("auth_date"), 10, 64)
	if err != nil || seconds <= 0 {
		return empty, "", ErrTelegram
	}
	issued := time.Unix(seconds, 0)
	if !now.Before(issued.Add(TelegramLifetime)) || issued.After(now.Add(FutureTolerance)) {
		return empty, "", ErrTelegram
	}
	hash := query.Get("hash")
	if !validToken(hash) {
		return empty, "", ErrTelegram
	}
	// The library verifies the signature. Freshness uses the same injected clock
	// as the rest of this operation and also rejects future auth_date values.
	if err = initdata.Validate(raw, bot, 0); err != nil {
		return empty, "", ErrTelegram
	}
	var profile struct {
		ID           int64   `json:"id"`
		IsBot        bool    `json:"is_bot"`
		FirstName    string  `json:"first_name"`
		Username     *string `json:"username"`
		LastName     *string `json:"last_name"`
		PhotoURL     *string `json:"photo_url"`
		LanguageCode *string `json:"language_code"`
	}
	if err = json.Unmarshal([]byte(query.Get("user")), &profile); err != nil || profile.ID <= 0 || profile.FirstName == "" || profile.IsBot {
		return empty, "", ErrTelegram
	}
	return userlogic.VerifiedTelegram{TelegramUserID: profile.ID, FirstName: profile.FirstName, Username: profile.Username, LastName: profile.LastName, PhotoURL: profile.PhotoURL, LanguageCode: profile.LanguageCode}, hash, nil
}
func (s *Service) VerifyTelegram(ctx context.Context, request, raw, source string, intent Intent) (*Result, error) {
	if !s.cfg.TelegramEnabled {
		return nil, ErrUnavailable
	}
	if !intent.valid() || intent.Purpose == userlogic.Unbind {
		return nil, ErrInvalid
	}
	ip, err := sourceIP(source)
	if err != nil {
		return nil, err
	}
	// Limit untrusted requests by source before signature verification.
	if err = s.limit.Allow(ctx, RateTelegram, "", ip); err != nil {
		return nil, err
	}
	profile, hash, err := verifyTelegram(raw, s.cfg.BotToken, s.now())
	if err != nil {
		return nil, err
	}
	return s.run(ctx, request, intent, usermodel.IdentityTelegram, s.digest("telegram", intent, hash), func(ctx context.Context, session sqlx.Session) (*userlogic.Resolved, string, error) {
		var result *userlogic.Resolved
		var err error
		if intent.Purpose == userlogic.Login {
			result, err = s.users.ResolveOrCreateVerifiedTelegramInSession(ctx, session, profile)
		} else {
			result, err = s.users.ChangeIdentityInSession(ctx, session, intent.Actor, intent.Purpose, usermodel.IdentityTelegram, "", &profile)
		}
		return result, s.digest("telegram-proof", hash), err
	})
}
