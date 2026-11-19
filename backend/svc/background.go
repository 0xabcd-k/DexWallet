package svc

import (
	"context"
	"errors"

	"github.com/0xabcd-k/DexWallet/backend/background"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	"github.com/apache/rocketmq-client-go/v2/primitive"
)

// StartBackground is explicit: CLI tools and HTTP unit fixtures do not launch
// workers just by assembling shared dependencies. No notification channel exists
// yet; production publishes facts, and registered business handlers come later.
func (d *ServiceContext) StartBackground(ctx context.Context) error {
	if err := d.startEvents(ctx); err != nil {
		return err
	}
	if d.Config.TONRecovery == nil {
		return nil
	}
	w, err := d.Config.TONRecovery.Wallet()
	if err != nil {
		return err
	}
	executor, err := chainsend.NewTON(d.TONSendIntents, w, nil, d.ChainAssets)
	if err != nil {
		return err
	}
	recovery, err := chainsend.NewRecovery(d.DB, executor, d.TONReceipts, *d.Config.TONRecovery)
	if err != nil {
		return err
	}
	d.TONRecovery = chainsend.NewRecoveryRuntime(recovery)
	return d.TONRecovery.Start(ctx)
}
func (d *ServiceContext) startEvents(ctx context.Context) error {
	if d.Config.Background == nil {
		return nil
	}
	store, err := background.NewStore(d.DB, *d.Config.Background)
	if err != nil {
		return err
	}
	codec, err := idcodec.New(d.Config.IDSecret, idcodec.Event)
	if err != nil {
		return err
	}
	d.Background, err = background.NewRuntime(store, d.Producer, codec, d.Namespace, nil)
	if err != nil {
		return err
	}
	return d.Background.Start(ctx, d.Config.RocketMQ.NameServers, primitive.Credentials{AccessKey: d.Config.RocketMQ.AccessKey, SecretKey: d.Config.RocketMQ.SecretKey})
}

// Admission closes for both pools before either waits. Dependencies outlive all
// in-flight RPC and SQL work, including partial startup or a timed-out drain.
func (d *ServiceContext) StopBackground() {
	if d.Background != nil {
		d.Background.Stop()
	}
	if d.TONRecovery != nil {
		d.TONRecovery.Stop()
	}
}
func (d *ServiceContext) WaitBackground(ctx context.Context) error {
	var err error
	if d.Background != nil {
		err = errors.Join(err, d.Background.Wait(ctx))
	}
	if d.TONRecovery != nil {
		err = errors.Join(err, d.TONRecovery.Wait(ctx))
	}
	return err
}
