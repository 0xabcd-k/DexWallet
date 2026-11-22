package svc

import (
	"context"
	"errors"

	"github.com/0xabcd-k/DexWallet/backend/background"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainreceipt"
	"github.com/0xabcd-k/DexWallet/backend/logic/chainsend"
	"github.com/apache/rocketmq-client-go/v2/primitive"
)

// StartBackground is explicit: CLI tools and HTTP unit fixtures do not launch
// workers just by assembling shared dependencies. No notification channel exists
// yet; production publishes facts, and registered business handlers come later.
func (d *ServiceContext) StartBackground(ctx context.Context) error {
	if c := d.Config.TRONReceiptListener; c != nil {
		listener := chainreceipt.NewTRONListener(d.DB, d.ChainAssets)
		var err error
		d.TRONListener, err = listener.Start(ctx, *c)
		if err != nil {
			return err
		}
	}
	if c := d.Config.BaseReceiptListener; c != nil {
		listener := chainreceipt.NewBaseListener(d.DB, d.ChainAssets, d.BaseReceipts, d.ReceiveAttribution)
		var err error
		d.BaseListener, err = listener.Start(ctx, *c)
		if err != nil {
			return err
		}
	}
	if c := d.Config.SolanaReceiptListener; c != nil {
		listener := chainreceipt.NewSolanaListener(d.DB, d.ChainAssets, d.ChainReceipts, d.ReceiveAttribution)
		var err error
		d.SolanaListener, err = listener.Start(ctx, *c)
		if err != nil {
			return err
		}
	}
	if c := d.Config.TONReceiptListener; c != nil {
		listener := chainreceipt.NewTONListener(d.DB, d.ChainAssets, d.TONReceipts, d.ReceiveAttribution)
		var err error
		d.TONListener, err = listener.Start(ctx, *c)
		if err != nil {
			return err
		}
	}
	if err := d.startEvents(ctx); err != nil {
		return err
	}
	if err := d.startTONRecovery(ctx); err != nil {
		return err
	}
	if err := d.startSolanaRecovery(ctx); err != nil {
		return err
	}
	if err := d.startBaseRecovery(ctx); err != nil {
		return err
	}
	if d.Config.TRONRecovery == nil {
		return nil
	}
	recovery, err := chainsend.NewTRONRecovery(d.DB, d.ChainAssets, d.TRONReceipts, *d.Config.TRONRecovery)
	if err != nil {
		return err
	}
	d.TRONRecovery = chainsend.NewRecoveryRuntime(recovery)
	return d.TRONRecovery.Start(ctx)
}
func (d *ServiceContext) startBaseRecovery(ctx context.Context) error {
	if d.Config.BaseRecovery == nil {
		return nil
	}
	recovery, err := chainsend.NewBaseRecovery(d.DB, d.ChainAssets, d.BaseReceipts, *d.Config.BaseRecovery)
	if err != nil {
		return err
	}
	d.BaseRecovery = chainsend.NewRecoveryRuntime(recovery)
	return d.BaseRecovery.Start(ctx)
}
func (d *ServiceContext) startSolanaRecovery(ctx context.Context) error {
	if d.Config.SolanaRecovery == nil {
		return nil
	}
	recovery, err := chainsend.NewSolanaRecovery(d.DB, d.ChainAssets, d.ChainReceipts, *d.Config.SolanaRecovery)
	if err != nil {
		return err
	}
	d.SolanaRecovery = chainsend.NewRecoveryRuntime(recovery)
	return d.SolanaRecovery.Start(ctx)
}
func (d *ServiceContext) startTONRecovery(ctx context.Context) error {
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

// Admission closes for every pool before waiting. Dependencies outlive all
// in-flight RPC and SQL work, including partial startup or a timed-out drain.
func (d *ServiceContext) StopBackground() {
	if d.TRONListener != nil {
		d.TRONListener.Stop()
	}
	if d.BaseListener != nil {
		d.BaseListener.Stop()
	}
	if d.SolanaListener != nil {
		d.SolanaListener.Stop()
	}
	if d.TONListener != nil {
		d.TONListener.Stop()
	}
	if d.Background != nil {
		d.Background.Stop()
	}
	if d.TONRecovery != nil {
		d.TONRecovery.Stop()
	}
	if d.TRONRecovery != nil {
		d.TRONRecovery.Stop()
	}
	if d.BaseRecovery != nil {
		d.BaseRecovery.Stop()
	}
	if d.SolanaRecovery != nil {
		d.SolanaRecovery.Stop()
	}
}
func (d *ServiceContext) WaitBackground(ctx context.Context) error {
	var err error
	if d.TRONListener != nil {
		err = errors.Join(err, d.TRONListener.Wait(ctx))
	}
	if d.BaseListener != nil {
		err = errors.Join(err, d.BaseListener.Wait(ctx))
	}
	if d.SolanaListener != nil {
		err = errors.Join(err, d.SolanaListener.Wait(ctx))
	}
	if d.TONListener != nil {
		err = errors.Join(err, d.TONListener.Wait(ctx))
	}
	if d.Background != nil {
		err = errors.Join(err, d.Background.Wait(ctx))
	}
	if d.TONRecovery != nil {
		err = errors.Join(err, d.TONRecovery.Wait(ctx))
	}
	if d.TRONRecovery != nil {
		err = errors.Join(err, d.TRONRecovery.Wait(ctx))
	}
	if d.BaseRecovery != nil {
		err = errors.Join(err, d.BaseRecovery.Wait(ctx))
	}
	if d.SolanaRecovery != nil {
		err = errors.Join(err, d.SolanaRecovery.Wait(ctx))
	}
	return err
}
