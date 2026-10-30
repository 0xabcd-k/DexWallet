package backoffice

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"time"

	auth "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	authmodel "github.com/0xabcd-k/DexWallet/backend/model/auth"
	model "github.com/0xabcd-k/DexWallet/backend/model/backoffice"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"golang.org/x/crypto/bcrypt"
)

func (s *Service) digest(parts ...string) string {
	raw, _ := json.Marshal(parts)
	h := hmac.New(sha256.New, []byte(s.secret))
	h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}
func (s *Service) password(ctx context.Context, m *store, r Realm, email, password string) (*Account, error) {
	u, err := m.email(ctx, r, email, true)
	if errors.Is(err, sqlx.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummy, []byte(password))
		return nil, auth.ErrCode
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil || u.Status != usermodel.StatusActive {
		return nil, auth.ErrCode
	}
	return u, nil
}
func (s *Service) loginInput(ctx context.Context, r Realm, email, password, source string, phase auth.RateAction) (string, error) {
	if s.mail == nil || s.limits[r] == nil {
		return "", auth.ErrUnavailable
	}
	email, err := emailAddress(email)
	if err != nil {
		return "", err
	}
	if len(password) == 0 || len(password) > PasswordMaxBytes {
		return "", auth.ErrCode
	}
	ip, err := netip.ParseAddr(source)
	if err != nil {
		return "", auth.ErrInvalid
	}
	if err = s.limits[r].Allow(ctx, phase, email, ip.String()); err != nil {
		return "", err
	}
	return email, nil
}
func (s *Service) SendCode(ctx context.Context, r Realm, email, password, source, locale, trace string) (*auth.Challenge, error) {
	email, err := s.loginInput(ctx, r, email, password, source, auth.RateSend)
	if err != nil {
		return nil, err
	}
	var bytes [32]byte
	if _, err = rand.Read(bytes[:]); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(bytes[:])
	n, err := rand.Int(rand.Reader, big.NewInt(auth.CodeSpace))
	if err != nil {
		return nil, err
	}
	code := fmt.Sprintf("%06d", n.Int64())
	// Match MySQL DATETIME(6) precision so the initial resend boundary is exact.
	now := time.Now().UTC().Truncate(time.Microsecond)
	expires := now.Add(auth.CodeLifetime)
	a := Actor{Realm: r, TraceID: trace}
	var u *Account
	err = s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		m := s.store.session(tx)
		var err error
		u, err = s.password(ctx, m, r, email, password)
		if err != nil {
			return err
		}
		a.ID = u.Id
		if _, err = m.conn.ExecCtx(ctx, "INSERT INTO backoffice_challenge(realm,email,issued_at,expires_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id)", r, email, now.Add(-auth.ResendInterval), now); err != nil {
			return err
		}
		var row model.BackofficeChallenge
		if err = m.conn.QueryRowCtx(ctx, &row, "SELECT * FROM backoffice_challenge WHERE realm=? AND email=? FOR UPDATE", r, email); err != nil {
			return err
		}
		if now.Before(row.IssuedAt.Add(auth.ResendInterval)) {
			return auth.ErrRateLimited
		}
		row.ChallengeToken = token
		row.CodeDigest = s.digest(string(r), email, token, code)
		row.AuthVersion = u.AuthVersion
		row.Status = authmodel.ChallengePending
		row.Attempts = 0
		row.IssuedAt = now
		row.ExpiresAt = expires
		return model.NewBackofficeChallengeModel(m.conn).Update(ctx, &row)
	})
	if err != nil {
		return nil, s.failure(ctx, a, ActionCode, r, &Account{Email: email}, "", err)
	}
	delivery := s.mail.SendCode(ctx, email, code, locale)
	state := authmodel.ChallengeActive
	if delivery != nil {
		state = authmodel.ChallengeFailed
	}
	call, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	result, err := s.store.conn.ExecCtx(call, "UPDATE backoffice_challenge SET status=? WHERE realm=? AND email=? AND challenge_token=? AND status=?", state, r, email, token, authmodel.ChallengePending)
	if err == nil {
		var count int64
		count, err = result.RowsAffected()
		if err == nil && count != 1 {
			err = auth.ErrCode
		}
	}
	if delivery != nil || err != nil {
		return nil, s.failure(ctx, a, ActionCode, r, u, "", fmt.Errorf("%w: %w", auth.ErrUnavailable, errors.Join(delivery, err)))
	}
	return &auth.Challenge{Token: token, ExpiresAt: expires}, nil
}
func (s *Service) Login(ctx context.Context, r Realm, email, password, challenge, code, source, trace string) (*Account, error) {
	email, err := s.loginInput(ctx, r, email, password, source, auth.RateVerify)
	if err != nil {
		return nil, err
	}
	if len(challenge) != 64 || len(code) != 6 {
		return nil, auth.ErrCode
	}
	a := Actor{Realm: r, TraceID: trace}
	var u *Account
	var rejected error
	err = s.tx.TransactCtx(ctx, func(ctx context.Context, tx sqlx.Session) error {
		m := s.store.session(tx)
		var err error
		u, err = s.password(ctx, m, r, email, password)
		if err != nil {
			return err
		}
		a.ID = u.Id
		var row model.BackofficeChallenge
		err = m.conn.QueryRowCtx(ctx, &row, "SELECT * FROM backoffice_challenge WHERE realm=? AND email=? FOR UPDATE", r, email)
		if errors.Is(err, sqlx.ErrNotFound) {
			return auth.ErrCode
		}
		if err != nil {
			return err
		}
		if row.ChallengeToken != challenge || row.AuthVersion != u.AuthVersion || row.Status != authmodel.ChallengeActive || !time.Now().Before(row.ExpiresAt) || row.Attempts >= auth.MaxCodeAttempts {
			return auth.ErrCode
		}
		if !hmac.Equal([]byte(row.CodeDigest), []byte(s.digest(string(r), email, challenge, code))) {
			row.Attempts++
			rejected = auth.ErrCode
		} else {
			row.Status = authmodel.ChallengeConsumed
		}
		if err = model.NewBackofficeChallengeModel(m.conn).Update(ctx, &row); err != nil {
			return err
		}
		result := ResultSuccess
		if rejected != nil {
			result = ResultFailure
		}
		return s.audit(ctx, m, a, ActionLogin, r, u, "", "", "", result)
	})
	if err != nil {
		return nil, s.failure(ctx, a, ActionLogin, r, &Account{Email: email}, "", err)
	}
	if rejected != nil {
		return nil, rejected
	}
	return u, nil
}

// Login and password changes are atomic but do not promise replay of a consumed
// code: after an unknown response, obtain a new code and authenticate again.
