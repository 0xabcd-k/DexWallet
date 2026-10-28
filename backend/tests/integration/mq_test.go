//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/0xabcd-k/DexWallet/backend/internal/testenv"
	rocketmq "github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/admin"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
)

func TestRocketMQDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	topic := testRun.Name("topic", "delivery")
	group := testRun.Name("group", "consumer")
	a, err := admin.NewAdmin(admin.WithResolver(primitive.NewPassthroughResolver([]string{testenv.NameServer})))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for _, v := range []testenv.Resource{{Kind: "topic", Name: topic}, {Kind: "retry", Name: testRun.Name("retry", "")}, {Kind: "dlq", Name: testRun.Name("dlq", "")}, {Kind: "group", Name: group}} {
		if err = testRun.Record(v.Kind, v.Name); err != nil {
			t.Fatal(err)
		}
	}
	if err = a.CreateTopic(ctx, admin.WithTopicCreate(topic), admin.WithBrokerAddrCreate(testenv.BrokerAddress), admin.WithReadQueueNums(1), admin.WithWriteQueueNums(1)); err != nil {
		t.Fatal(err)
	}
	received := make(chan string, 1)
	c, err := rocketmq.NewPushConsumer(consumer.WithNameServer(primitive.NamesrvAddr{testenv.NameServer}), consumer.WithGroupName(group), consumer.WithInstance(group), consumer.WithConsumeFromWhere(consumer.ConsumeFromFirstOffset), consumer.WithConsumeGoroutineNums(1))
	if err != nil {
		t.Fatal(err)
	}
	if err = c.Subscribe(topic, consumer.MessageSelector{}, func(_ context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
		for _, msg := range msgs {
			if string(msg.Body) == testRun.ID {
				select {
				case received <- msg.MsgId:
				default:
				}
			}
		}
		return consumer.ConsumeSuccess, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err = c.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := c.Shutdown(); e != nil {
			t.Error(e)
		}
	}()
	p, err := rocketmq.NewProducer(producer.WithNameServer(primitive.NamesrvAddr{testenv.NameServer}), producer.WithGroupName(testRun.Name("group", "producer")), producer.WithInstanceName(testRun.ID), producer.WithRetry(0))
	if err != nil {
		t.Fatal(err)
	}
	if err = p.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := p.Shutdown(); e != nil {
			t.Error(e)
		}
	}()
	result, err := p.SendSync(ctx, primitive.NewMessage(topic, []byte(testRun.ID)))
	if err != nil {
		t.Fatalf("send result unknown: %v", err)
	}
	if result.Status != primitive.SendOK {
		t.Fatalf("send status: %v", result.Status)
	}
	select {
	case id := <-received:
		t.Logf("delivered topic=%s message=%s run=%s", topic, id, testRun.ID)
	case <-ctx.Done():
		t.Fatal("no matching message received before deadline")
	}
}
