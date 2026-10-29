package httpapi

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	"github.com/0xabcd-k/DexWallet/backend/apperror"
	authlogic "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/session"
)

type authKey struct{}
type sourceKey struct{}

// Authorization is request-scoped. Actor always comes from a verified JWT;
// Current is populated once by ordinary authentication, never by request JSON.
type Authorization struct {
	Actor   userlogic.Actor
	Claims  *session.Claims
	Current *usermodel.User
	Renewal string
}

func WithAuthorization(ctx context.Context, a Authorization) context.Context {
	return context.WithValue(ctx, authKey{}, a)
}
func Authorized(ctx context.Context) (Authorization, error) {
	a, ok := ctx.Value(authKey{}).(Authorization)
	if !ok || a.Actor.UserID <= 0 || a.Actor.AuthVersion <= 0 {
		return Authorization{}, apperror.New(apperror.Unauthenticated, nil)
	}
	return a, nil
}
func WithSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, sourceKey{}, source)
}
func Source(ctx context.Context) string { source, _ := ctx.Value(sourceKey{}).(string); return source }

func ChallengeReply(ctx context.Context, c *authlogic.Challenge, err error) (*types.ChallengeResponse, error) {
	if err != nil {
		return nil, err
	}
	return &types.ChallengeResponse{Code: SuccessCode, Message: "OK", TraceId: TraceID(ctx), Data: types.ChallengeData{Challenge: c.Token, ExpiresAt: c.ExpiresAt.Unix()}}, nil
}

func TokenReply(ctx context.Context, tokens *session.Tokens, r *authlogic.Result, err error) (*types.TokenResponse, error) {
	if err != nil {
		return nil, err
	}
	// The operation service has already checked the current row/version, including
	// receipt recovery. Issue from that result, never from an ordinary user cache.
	raw, claims, err := tokens.Issue(r.User.Id, r.User.AuthVersion)
	if err != nil {
		return nil, err
	}
	return &types.TokenResponse{Code: SuccessCode, Message: "OK", TraceId: TraceID(ctx), Data: types.TokenData{AccessToken: raw, UserIdx: claims.Subject, ExpiresAt: claims.ExpiresAt.Unix()}}, nil
}
