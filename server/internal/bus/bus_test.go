package bus

import (
	"testing"
	"time"
)

func TestPublishSubscribe(t *testing.T) {
	b := New(10)

	sub := b.Subscribe(10, "ohlc.binance.*")

	b.Publish(Message{Topic: "ohlc.binance.BTCUSD", Payload: "candle1"})
	b.Publish(Message{Topic: "ohlc.kraken.BTCUSD", Payload: "candle2"})  // should not match
	b.Publish(Message{Topic: "ohlc.binance.ETHUSD", Payload: "candle3"})

	// Give a moment for messages.
	time.Sleep(10 * time.Millisecond)

	received := drain(sub.C)
	if len(received) != 2 {
		t.Fatalf("expected 2 messages, got %d: %v", len(received), received)
	}
	if received[0].Payload != "candle1" {
		t.Errorf("expected candle1, got %v", received[0].Payload)
	}
	if received[1].Payload != "candle3" {
		t.Errorf("expected candle3, got %v", received[1].Payload)
	}

	b.Unsubscribe(sub)
}

func TestRingBufferReplay(t *testing.T) {
	b := New(5)

	// Publish before anyone subscribes.
	for i := 0; i < 7; i++ {
		b.Publish(Message{Topic: "trade.binance.BTCUSD", Payload: i})
	}

	sub := b.Subscribe(10, "trade.binance.*")
	time.Sleep(10 * time.Millisecond)

	received := drain(sub.C)
	// Ring depth 5, published 7, so we should get the last 5.
	if len(received) != 5 {
		t.Fatalf("expected 5 replayed messages, got %d", len(received))
	}
	if received[0].Payload != 2 {
		t.Errorf("expected first replayed payload to be 2, got %v", received[0].Payload)
	}

	b.Unsubscribe(sub)
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	b := New(10)
	sub := b.Subscribe(10, "*")
	b.Unsubscribe(sub)

	_, ok := <-sub.C
	if ok {
		t.Error("expected channel to be closed")
	}
}

func TestNoMatchNoDelivery(t *testing.T) {
	b := New(10)
	sub := b.Subscribe(10, "ohlc.binance.*")

	b.Publish(Message{Topic: "news", Payload: "headline"})
	time.Sleep(10 * time.Millisecond)

	received := drain(sub.C)
	if len(received) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(received))
	}

	b.Unsubscribe(sub)
}

func drain(ch chan Message) []Message {
	var msgs []Message
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return msgs
			}
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}
