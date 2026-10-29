package svc

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/health"
	"github.com/zeromicro/go-zero/core/logx"
)

func (s *ServiceContext) startHealth(ctx context.Context) error {
	c := s.Config
	s.Health = health.New(s.checks, c.ProbeFreshness)
	s.Health.Refresh(ctx, s.checks, c.ProbeTimeout, reportHealth)
	if err := ctx.Err(); err != nil {
		return err
	}
	// Startup cancellation must not stop diagnostics during HTTP drain. Close owns
	// this loop and waits for its last probe before releasing the clients it uses.
	probeCtx, cancel := context.WithCancel(context.Background())
	s.stopProbe = cancel
	s.probeDone = make(chan struct{})
	go func() {
		defer close(s.probeDone)
		s.Health.Run(probeCtx, s.checks, c.ProbeInterval, c.ProbeTimeout, reportHealth)
	}()
	return nil
}

func reportHealth(s health.State) {
	fields := []logx.LogField{logx.Field("dependency", s.Name), logx.Field("required", s.Required)}
	if s.Err != nil {
		logx.Errorw("dependency unavailable", append(fields, logx.Field("cause", s.Err.Error()))...)
	} else {
		logx.Infow("dependency available", fields...)
	}
}
