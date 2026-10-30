// background is a local operations tool; it starts no API, publisher or consumer.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/background"
	"github.com/0xabcd-k/DexWallet/backend/config"
	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	"github.com/0xabcd-k/DexWallet/backend/svc"
)

func main() {
	if err := execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func execute() (err error) {
	path := flag.String("f", "api/etc/platform.yaml", "application configuration")
	action := flag.String("action", "list", "list or retry")
	queue := flag.String("queue", "events", "events or tasks")
	state := flag.String("state", "dead", "list state; empty for all states")
	limit := flag.Int("limit", 20, "list limit 1..100")
	idx := flag.String("idx", "", "event/task idx for retry")
	reason := flag.String("reason", "", "required reason for manual retry")
	flag.Parse()
	if *action != "list" && *action != "retry" {
		return fmt.Errorf("action must be list or retry")
	}
	q := background.Queue(*queue)
	kind := idcodec.Event
	if q == background.Tasks {
		kind = idcodec.BackgroundTask
	} else if q != background.Events {
		return fmt.Errorf("queue must be events or tasks")
	}
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	policy := background.Defaults()
	if c.Background != nil {
		policy = *c.Background
	}
	c.Background = nil
	c.RocketMQ.Enabled = false
	c.RocketMQ.Required = false
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := svc.NewServiceContext(ctx, c)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	store, err := background.NewStore(s.DB, policy)
	if err != nil {
		return err
	}
	codec, err := idcodec.New(c.IDSecret, kind)
	if err != nil {
		return err
	}
	if *action == "retry" {
		id, err := codec.Decode(*idx)
		if err != nil {
			return err
		}
		operator, err := user.Current()
		if err != nil {
			return err
		}
		if err = store.Retry(ctx, q, id, operator.Username, *reason); err != nil {
			return err
		}
		fmt.Printf("requeued %s idx=%s operator=%s reason=%s\n", q, *idx, operator.Username, *reason)
		return nil
	}
	records, err := store.List(ctx, q, *state, *limit)
	if err != nil {
		return err
	}
	output := make([]map[string]any, 0, len(records))
	for _, r := range records {
		idx, err := codec.Encode(r.ID)
		if err != nil {
			return err
		}
		eventCodec, err := idcodec.New(c.IDSecret, idcodec.Event)
		if err != nil {
			return err
		}
		eventIdx, err := eventCodec.Encode(r.EventID)
		if err != nil {
			return err
		}
		output = append(output, map[string]any{"idx": idx, "state": r.State, "attempts": r.Attempts, "generation": r.Generation, "lastError": r.LastError, "retryOperator": r.RetryOperator, "retryReason": r.RetryReason, "eventIdx": eventIdx, "kind": r.Kind, "businessId": r.BusinessID, "handler": r.Handler, "nextAt": r.NextAt, "leaseUntil": r.LeaseUntil})
	}
	return json.NewEncoder(os.Stdout).Encode(output)
}
