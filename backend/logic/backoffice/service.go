package backoffice

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/apperror"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	auth "github.com/0xabcd-k/DexWallet/backend/logic/auth"
	userlogic "github.com/0xabcd-k/DexWallet/backend/logic/user"
	model "github.com/0xabcd-k/DexWallet/backend/model/backoffice"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/readcache"
	"github.com/0xabcd-k/DexWallet/backend/session"
	"github.com/0xabcd-k/DexWallet/backend/transaction"
	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
	"golang.org/x/crypto/bcrypt"
)

const (
	ActionLogin      = "login"
	ActionCode       = "send_code"
	ActionCreate     = "create_account"
	ActionPassword   = "change_password"
	ActionReset      = "reset_password"
	ActionStatus     = "change_status"
	ResultSuccess    = "success"
	ResultFailure    = "failure"
	ResultUnknown    = "unknown"
	PasswordMinBytes = 12
	PasswordMaxBytes = 72
)

type Config struct {
	AdminSecret    string `json:",optional"`
	MerchantSecret string `json:",optional"`
}

func (c Config) Check() error {
	if len(c.AdminSecret) < 32 || len(c.MerchantSecret) < 32 || c.AdminSecret == c.MerchantSecret {
		return errors.New("independent admin and merchant token secrets of at least 32 bytes required")
	}
	return nil
}

type Actor struct {
	Realm       Realm
	ID, Version int64
	TraceID     string
}
type Authorization struct {
	Actor   Actor
	Account *Account
	Renewal string
}
type authKey struct{}

func WithAuthorization(ctx context.Context, a Authorization) context.Context {
	return context.WithValue(ctx, authKey{}, a)
}
func Authorized(ctx context.Context) (Authorization, error) {
	a, ok := ctx.Value(authKey{}).(Authorization)
	if !ok {
		return a, session.ErrInvalid
	}
	return a, nil
}

type Service struct {
	store   *store
	tx      *transaction.Runner
	users   *userlogic.Service
	UserIdx *idcodec.Codec
	codecs  map[Realm]*idcodec.Codec
	tokens  map[Realm]*session.Tokens
	mail    auth.Mailer
	limits  map[Realm]auth.Limiter
	proofs  *auth.Proofs
	dummy   []byte
}

func New(c Config, cfg auth.Config, conn sqlx.SqlConn, tx *transaction.Runner, users *userlogic.Service, userIdx *idcodec.Codec, client *redis.Client, namespace, idSecret string, mail auth.Mailer) (*Service, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	if client == nil || tx == nil {
		return nil, auth.ErrUnavailable
	}
	s := &Service{store: &store{conn: conn, cache: readcache.New[Account](client), prefix: namespace + accountCachePrefix}, tx: tx, users: users, UserIdx: userIdx, codecs: map[Realm]*idcodec.Codec{}, tokens: map[Realm]*session.Tokens{}, mail: mail, limits: map[Realm]auth.Limiter{}, proofs: auth.NewProofs(client, namespace, cfg.Secret)}
	for _, r := range []Realm{Admin, Merchant} {
		kind, secret, aud := idcodec.Admin, c.AdminSecret, session.AdminAudience
		if r == Merchant {
			kind, secret, aud = idcodec.Merchant, c.MerchantSecret, session.MerchantAudience
		}
		codec, err := idcodec.New(idSecret, kind)
		if err != nil {
			return nil, err
		}
		s.codecs[r] = codec
		s.tokens[r], err = session.NewForAudience(session.Config{Secret: secret}, namespace, codec, aud)
		if err != nil {
			return nil, err
		}
		s.limits[r] = auth.NewRedisLimiter(client, namespace+":backoffice:"+string(r), cfg)
	}
	var err error
	s.dummy, err = bcrypt.GenerateFromPassword([]byte("nonexistent-account-dummy-password"), bcrypt.DefaultCost)
	return s, err
}
func (s *Service) Idx(r Realm, id int64) (string, error) {
	if id == 0 {
		return "", nil
	}
	if r == Product {
		return s.UserIdx.Encode(id)
	}
	c := s.codecs[r]
	if c == nil {
		return "", apperror.New(apperror.InvalidArgument, nil)
	}
	return c.Encode(id)
}
func (s *Service) Decode(r Realm, idx string) (int64, error) {
	if r == Product {
		return s.UserIdx.Decode(idx)
	}
	c := s.codecs[r]
	if c == nil {
		return 0, apperror.New(apperror.InvalidArgument, nil)
	}
	return c.Decode(idx)
}
func (s *Service) Issue(r Realm, u *Account) (string, *session.Claims, error) {
	t := s.tokens[r]
	if t == nil {
		return "", nil, session.ErrInvalid
	}
	return t.Issue(u.Id, u.AuthVersion)
}
func (s *Service) Authenticate(ctx context.Context, r Realm, raw, trace string) (Authorization, error) {
	t := s.tokens[r]
	if t == nil {
		return Authorization{}, session.ErrInvalid
	}
	claims, id, err := t.Parse(raw)
	if err != nil {
		return Authorization{}, err
	}
	u, err := s.store.cached(ctx, r, id)
	if errors.Is(err, sqlx.ErrNotFound) {
		err = session.ErrInvalid
	}
	if err != nil {
		return Authorization{}, err
	}
	if u.Status != usermodel.StatusActive || u.AuthVersion != claims.AuthVersion {
		return Authorization{}, session.ErrInvalid
	}
	renew, err := t.Renewal(claims, id)
	return Authorization{Actor: Actor{Realm: r, ID: id, Version: claims.AuthVersion, TraceID: trace}, Account: u, Renewal: renew}, err
}
func (s *Service) check(ctx context.Context, m *store, a Actor, management, lock bool) (*Account, error) {
	if a.Realm == Super {
		if management {
			return nil, nil
		}
		return nil, session.ErrInvalid
	}
	if management && a.Realm != Admin {
		return nil, apperror.New(apperror.PermissionDenied, nil)
	}
	var u *Account
	var err error
	if !lock {
		u, err = m.cached(ctx, a.Realm, a.ID)
	} else {
		u, err = m.find(ctx, a.Realm, a.ID, true)
	}
	if errors.Is(err, sqlx.ErrNotFound) {
		return nil, session.ErrInvalid
	}
	if err != nil {
		return nil, err
	}
	if u.Status != usermodel.StatusActive || u.AuthVersion != a.Version {
		return nil, session.ErrInvalid
	}
	if management && u.MustChangePassword != 0 {
		return nil, apperror.New(apperror.PermissionDenied, nil)
	}
	return u, nil
}
func passwordHash(password string) (string, error) {
	if len(password) < PasswordMinBytes || len(password) > PasswordMaxBytes {
		return "", apperror.New(apperror.InvalidArgument, nil)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}
func reasonOK(reason string) bool {
	return strings.TrimSpace(reason) != "" && len([]rune(reason)) <= 512
}
func bump(u *Account) error {
	if u.AuthVersion == math.MaxInt64 {
		return apperror.New(apperror.Conflict, errors.New("authentication version exhausted"))
	}
	u.AuthVersion++
	return nil
}
func (s *Service) audit(ctx context.Context, m *store, a Actor, action string, r Realm, u *Account, before, after, reason, result string) error {
	row := &model.BackofficeAudit{ActorRealm: string(a.Realm), ActorId: a.ID, Action: action, TargetRealm: string(r), BeforeValue: before, AfterValue: after, Reason: reason, Result: result, TraceId: a.TraceID}
	if u != nil {
		row.TargetId = u.Id
		row.TargetEmail = u.Email
	}
	_, err := model.NewBackofficeAuditModel(m.conn).Insert(ctx, row)
	return err
}

// Login and failed operations are diagnostic; their audit must not replace the result.
func (s *Service) record(ctx context.Context, a Actor, action string, r Realm, u *Account, reason, result string) {
	call, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := s.audit(call, s.store, a, action, r, u, "", "", reason, result); err != nil {
		logx.WithContext(ctx).Errorw("backoffice audit failed", logx.Field("action", action), logx.Field("realm", r), logx.Field("cause", err))
	}
}
func (s *Service) failure(ctx context.Context, a Actor, action string, r Realm, u *Account, reason string, cause error) error {
	result := ResultFailure
	var txFailure *transaction.Failure
	if errors.As(cause, &txFailure) && txFailure.Unknown() {
		result = ResultUnknown
	}
	s.record(ctx, a, action, r, u, reason, result)
	return cause
}
func (s *Service) List(ctx context.Context, a Actor, r Realm, page int64) ([]Account, error) {
	if _, err := s.check(ctx, s.store, a, true, false); err != nil {
		return nil, err
	}
	return s.store.list(ctx, r, page)
}
func (s *Service) Audits(ctx context.Context, a Actor, page int64) ([]model.BackofficeAudit, error) {
	if page < 1 || page > 1000000 {
		return nil, apperror.New(apperror.InvalidArgument, nil)
	}
	if _, err := s.check(ctx, s.store, a, true, false); err != nil {
		return nil, err
	}
	rows := []model.BackofficeAudit{}
	err := s.store.conn.QueryRowsCtx(ctx, &rows, "SELECT * FROM backoffice_audit ORDER BY id DESC LIMIT ? OFFSET ?", PageSize, (page-1)*PageSize)
	return rows, err
}
