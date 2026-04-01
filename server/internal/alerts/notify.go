package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/notbbg/notbbg/server/internal/bus"
)

// NotifyChannel defines a notification destination.
type NotifyChannel struct {
	Type      string // "slack", "telegram", "kakao"
	Webhook   string // Slack webhook URL
	BotToken  string // Telegram bot token
	ChatID    string // Telegram chat ID
	RateLimit time.Duration
}

// Notifier sends alert notifications to external messaging services.
type Notifier struct {
	bus      *bus.Bus
	channels []NotifyChannel
	mu       sync.Mutex
	lastSent map[string]time.Time // alert ID -> last notification time
}

// NewNotifier creates a notification dispatcher.
func NewNotifier(b *bus.Bus, channels []NotifyChannel) *Notifier {
	return &Notifier{
		bus:      b,
		channels: channels,
		lastSent: make(map[string]time.Time),
	}
}

// Run subscribes to alert events and dispatches notifications.
func (n *Notifier) Run(ctx context.Context) error {
	sub := n.bus.Subscribe(64, "alert")
	defer n.bus.Unsubscribe(sub)

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-sub.C:
			if !ok {
				return nil
			}
			alert, ok := msg.Payload.(Alert)
			if !ok {
				continue
			}
			n.dispatch(ctx, alert)
		}
	}
}

func (n *Notifier) dispatch(ctx context.Context, alert Alert) {
	n.mu.Lock()
	defer n.mu.Unlock()

	for _, ch := range n.channels {
		rateLimit := ch.RateLimit
		if rateLimit == 0 {
			rateLimit = 5 * time.Minute
		}

		key := fmt.Sprintf("%s:%s", alert.ID, ch.Type)
		if last, ok := n.lastSent[key]; ok && time.Since(last) < rateLimit {
			continue
		}

		var err error
		switch ch.Type {
		case "slack":
			err = n.sendSlack(ctx, ch.Webhook, alert)
		case "telegram":
			err = n.sendTelegram(ctx, ch.BotToken, ch.ChatID, alert)
		default:
			slog.Debug("unknown notification channel", "type", ch.Type)
			continue
		}

		if err != nil {
			slog.Warn("notification failed", "channel", ch.Type, "error", err)
			continue
		}

		n.lastSent[key] = time.Now()
		slog.Info("notification sent", "channel", ch.Type, "alert", alert.ID)
	}
}

func (n *Notifier) sendSlack(ctx context.Context, webhookURL string, alert Alert) error {
	text := formatAlertMessage(alert)
	payload := map[string]string{"text": text}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook returned %d", resp.StatusCode)
	}
	return nil
}

func (n *Notifier) sendTelegram(ctx context.Context, botToken, chatID string, alert Alert) error {
	text := formatAlertMessage(alert)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	payload := map[string]string{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API returned %d", resp.StatusCode)
	}
	return nil
}

func formatAlertMessage(alert Alert) string {
	typeStr := "Alert"
	switch alert.Type {
	case PriceAbove:
		typeStr = "Price Above"
	case PriceBelow:
		typeStr = "Price Below"
	case VolumeSpike:
		typeStr = "Volume Spike"
	case Keyword:
		typeStr = "Keyword Match"
	case FeedDown:
		typeStr = "Feed Down"
	}

	msg := fmt.Sprintf("*notbbg Alert*\n*Type:* %s\n", typeStr)
	if alert.Instrument != "" {
		msg += fmt.Sprintf("*Instrument:* %s\n", alert.Instrument)
	}
	if alert.Threshold != 0 {
		msg += fmt.Sprintf("*Threshold:* %.2f\n", alert.Threshold)
	}
	if alert.Keyword != "" {
		msg += fmt.Sprintf("*Keyword:* %s\n", alert.Keyword)
	}
	msg += fmt.Sprintf("*Time:* %s", alert.TriggeredAt.Format(time.RFC3339))
	return msg
}
