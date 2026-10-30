package background

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/0xabcd-k/DexWallet/backend/idcodec"
	model "github.com/0xabcd-k/DexWallet/backend/model/background"
	"github.com/apache/rocketmq-client-go/v2/primitive"
)

const EnvelopeVersion = 1
const TopicSuffix = "_business_events_v1"
const GroupSuffix = "_background_tasks_v1"
const MaxReconsumeTimes int32 = 3

type Sender interface {
	SendSync(context.Context, ...*primitive.Message) (*primitive.SendResult, error)
}
type Envelope struct {
	Version  int    `json:"version"`
	EventIdx string `json:"eventIdx"`
}

// DecodeEnvelope accepts only a compact identity. Business payload and routing
// are loaded from the primary, never trusted from an MQ message or its msgId.
func DecodeEnvelope(body []byte, codec *idcodec.Codec) (int64, error) {
	if len(body) > 512 {
		return 0, fmt.Errorf("event envelope exceeds 512 bytes")
	}
	var e Envelope
	if err := json.Unmarshal(body, &e); err != nil {
		return 0, err
	}
	if e.Version != EnvelopeVersion {
		return 0, fmt.Errorf("unsupported envelope version %d", e.Version)
	}
	return codec.Decode(e.EventIdx)
}

func (s *Store) PublishOne(ctx context.Context, sender Sender, codec *idcodec.Codec, topic string) (bool, error) {
	claim, err := s.Claim(ctx, Events, "")
	if err != nil || claim == nil {
		return false, err
	}
	row, err := s.Event(ctx, claim.ID)
	if err != nil {
		return true, err
	}
	idx, err := codec.Encode(row.Id)
	if err != nil {
		return true, err
	}
	body, err := json.Marshal(Envelope{EnvelopeVersion, idx})
	if err != nil {
		return true, err
	}
	message := primitive.NewMessage(topic, body)
	message.WithKeys([]string{idx})
	message.WithTag(row.Kind)
	result, sendErr := sender.SendSync(ctx, message)
	if sendErr == nil && (result == nil || result.Status != primitive.SendOK) {
		sendErr = fmt.Errorf("event %s publish not confirmed: %+v", idx, result)
	}
	if sendErr == nil {
		return true, s.finish(ctx, Events, *claim, model.Published, "")
	}
	state := model.Ready
	if claim.Attempts >= s.config.MaxAttempts {
		state = model.Dead
	}
	// Even a timeout may have reached the broker. Reuse the event idx on every send.
	return true, fmt.Errorf("publish event %s attempt %d: %w", idx, claim.Attempts, errors.Join(sendErr, s.finish(ctx, Events, *claim, state, sendErr.Error())))
}
