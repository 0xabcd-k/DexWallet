package backoffice

import (
	"context"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/httpapi"
	"github.com/0xabcd-k/DexWallet/backend/api/internal/types"
	domain "github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
)

func accountData(s *domain.Service, r domain.Realm, u *domain.Account) (types.BackofficeAccount, error) {
	idx, err := s.Idx(r, u.Id)
	return types.BackofficeAccount{Idx: idx, Email: u.Email, Status: u.Status, MustChangePassword: u.MustChangePassword != 0}, err
}
func accountReply(ctx context.Context, s *domain.Service, r domain.Realm, u *domain.Account, err error) (*types.BackofficeAccountResponse, error) {
	if err != nil {
		return nil, err
	}
	data, err := accountData(s, r, u)
	if err != nil {
		return nil, err
	}
	return &types.BackofficeAccountResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(ctx), Data: data}, nil
}
func tokenReply(ctx context.Context, s *domain.Service, r domain.Realm, u *domain.Account, err error) (*types.TokenResponse, error) {
	if err != nil {
		return nil, err
	}
	raw, c, err := s.Issue(r, u)
	if err != nil {
		return nil, err
	}
	return &types.TokenResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(ctx), Data: types.TokenData{AccessToken: raw, UserIdx: c.Subject, ExpiresAt: c.ExpiresAt.Unix()}}, nil
}
func productReply(ctx context.Context, s *domain.Service, u *usermodel.User, err error) (*types.ManagedUserResponse, error) {
	if err != nil {
		return nil, err
	}
	idx, err := s.Idx(domain.Product, u.Id)
	if err != nil {
		return nil, err
	}
	return &types.ManagedUserResponse{Code: httpapi.SuccessCode, Message: "OK", TraceId: httpapi.TraceID(ctx), Data: types.ManagedUserData{Idx: idx, Nickname: u.Nickname, Status: u.Status}}, nil
}
