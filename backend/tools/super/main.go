// super is a local server administration tool. It exposes no HTTP listener.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/logic/backoffice"
	usermodel "github.com/0xabcd-k/DexWallet/backend/model/user"
	"github.com/0xabcd-k/DexWallet/backend/svc"
)

const (
	actionCreate = "create-admin"
	actionReset  = "reset-admin"
	actionEnable = "enable-admin"
)

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func execute() (err error) {
	cfg := flag.String("f", "api/etc/platform.yaml", "API configuration file")
	action := flag.String("action", "", "create-admin, reset-admin, enable-admin")
	email := flag.String("email", "", "administrator email for create-admin")
	idx := flag.String("idx", "", "administrator idx for reset/enable")
	reason := flag.String("reason", "", "operation reason for audit")
	passwordStdin := flag.Bool("password-stdin", false, "read one password line from stdin; never pass passwords in arguments")
	flag.Parse()
	if *action != actionCreate && *action != actionReset && *action != actionEnable {
		return errors.New("choose --action create-admin|reset-admin|enable-admin")
	}
	if strings.TrimSpace(*reason) == "" {
		return errors.New("--reason is required")
	}
	password := ""
	if *action != actionEnable {
		if !*passwordStdin {
			return errors.New("--password-stdin is required")
		}
		reader := bufio.NewReader(io.LimitReader(os.Stdin, backoffice.PasswordMaxBytes+3))
		line, e := reader.ReadString('\n')
		if e != nil {
			return fmt.Errorf("read password line: %w", e)
		}
		password = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	}
	c, err := config.Load(*cfg)
	if err != nil {
		return err
	}
	if c.Backoffice == nil {
		return errors.New("Backoffice configuration is required")
	}
	// The command uses the shared dependency owner but has no use for MQ.
	c.RocketMQ.Enabled = false
	c.RocketMQ.Required = false
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := svc.NewServiceContext(ctx, c)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	actor := backoffice.Actor{Realm: backoffice.Super, TraceID: fmt.Sprintf("super-%d", time.Now().UnixNano())}
	var u *backoffice.Account
	switch *action {
	case actionCreate:
		u, err = s.Backoffice.Create(ctx, actor, backoffice.Admin, *email, password, *reason)
	case actionReset:
		u, err = s.Backoffice.ResetPassword(ctx, actor, backoffice.Admin, *idx, password, *reason)
	case actionEnable:
		u, err = s.Backoffice.SetStatus(ctx, actor, backoffice.Admin, *idx, usermodel.StatusActive, *reason)
	}
	if err != nil {
		return err
	}
	public, err := s.Backoffice.Idx(backoffice.Admin, u.Id)
	if err != nil {
		return err
	}
	fmt.Printf("%s: idx=%s email=%s status=%s\n", *action, public, u.Email, u.Status)
	return nil
}
