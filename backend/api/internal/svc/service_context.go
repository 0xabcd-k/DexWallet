// Code scaffolded by goctl. Safe to edit.
package svc

import (
	"github.com/0xabcd-k/DexWallet/backend/api/internal/config"
	"github.com/0xabcd-k/DexWallet/backend/health"
	base "github.com/0xabcd-k/DexWallet/backend/svc"
)

type ServiceContext struct {
	Config       config.Config
	Dependencies *base.Dependencies
	Health       *health.Monitor
}
