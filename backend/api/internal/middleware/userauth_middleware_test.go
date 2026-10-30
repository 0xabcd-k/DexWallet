package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/handler/account"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/svc"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/session"
	"github.com/golang-jwt/jwt/v4"
)

type requestUsers struct {
	usermodel.UserModel
	row   usermodel.User
	err   error
	calls int
}

func (f *requestUsers) FindOne(context.Context, int64) (*usermodel.User, error) {
	f.calls++
	return &f.row, f.err
}
func TestOrdinaryAuthenticationAndRequestUserReuse(t *testing.T) {
	httpapi.ConfigureErrors()
	idx, err := idcodec.New("test-only-key123", idcodec.User)
	if err != nil {
		t.Fatal(err)
	}
	const secret = "test-only-session-signing-secret-32"
	tokens, err := session.New(session.Config{Secret: secret}, "test", idx)
	if err != nil {
		t.Fatal(err)
	}
	raw, c, err := tokens.Issue(12, 7)
	if err != nil {
		t.Fatal(err)
	}
	c.IssuedAt = jwt.NewNumericDate(time.Now().Add(-37 * time.Hour))
	c.ExpiresAt = jwt.NewNumericDate(c.IssuedAt.Add(session.Lifetime))
	old, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, token, status string
		version             int64
		err                 error
		want, reads         int
	}{
		{name: "fresh", token: raw, status: usermodel.StatusActive, version: 7, want: 200, reads: 1},
		{name: "renew", token: old, status: usermodel.StatusActive, version: 7, want: 200, reads: 1},
		{name: "disabled", token: old, status: usermodel.StatusDisabled, version: 7, want: 403, reads: 1},
		{name: "new version", token: old, status: usermodel.StatusActive, version: 8, want: 401, reads: 1},
		{name: "missing user", token: raw, err: usermodel.ErrNotFound, want: 401, reads: 1},
		{name: "database failure", token: raw, err: errors.New("database-sentinel"), want: 500, reads: 1},
		{name: "no credential", want: 401, reads: 0},
		{name: "invalid token", token: "secret-sentinel", want: 401, reads: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &requestUsers{row: usermodel.User{Id: 12, Status: tc.status, AuthVersion: tc.version, Nickname: "one snapshot"}, err: tc.err}
			s := &svc.ServiceContext{Tokens: tokens, Users: userlogic.New(model, nil, nil, nil)}
			handler := NewUserAuthMiddleware(s).Handle(account.CurrentUserHandler(s))
			r := httptest.NewRequest(http.MethodGet, "/v1/user/current", nil)
			if tc.token != "" {
				r.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			handler(w, r)
			if w.Code != tc.want || model.calls != tc.reads {
				t.Fatalf("status=%d reads=%d", w.Code, model.calls)
			}
			header := w.Header().Get(session.TokenHeader)
			if tc.name == "renew" {
				claims, id, e := tokens.Parse(header)
				if e != nil || id != 12 || claims.AuthVersion != 7 {
					t.Fatal("invalid renewal", e)
				}
			} else if header != "" {
				t.Fatal("unexpected token on response")
			}
			if tc.want == 200 {
				var response types.CurrentUserResponse
				if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Data.Nickname != "one snapshot" {
					t.Fatal("request user not reused", err)
				}
			}
		})
	}
}
