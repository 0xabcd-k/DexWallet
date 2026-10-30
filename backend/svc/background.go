package svc

import (
	"context"

	"github.com/0xabcd-k/DexWallet/backend/background"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/apache/rocketmq-client-go/v2/primitive"
)

// StartBackground is explicit: CLI tools and HTTP unit fixtures do not launch
// workers just by assembling shared dependencies. No notification channel exists
// yet; production publishes facts, and registered business handlers come later.
func (d *ServiceContext) StartBackground(ctx context.Context) error {
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
