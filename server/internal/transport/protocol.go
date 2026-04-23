// Package transport defines the JSON wire protocol between server and clients.
// This is a simple framing protocol used until protobuf codegen is wired up.
package transport

import "encoding/json"

// MsgType identifies the type of a wire message.
type MsgType string

const (
	MsgSubscribe   MsgType = "subscribe"
	MsgUnsubscribe MsgType = "unsubscribe"
	MsgUpdate      MsgType = "update"
	MsgQuery       MsgType = "query"
	MsgQueryResult MsgType = "query_result"
	MsgPing        MsgType = "ping"
	MsgPong        MsgType = "pong"
	MsgPair        MsgType = "pair"
	MsgPairOK      MsgType = "pair_ok"
	MsgPairFail    MsgType = "pair_fail"
	MsgCreateAlert  MsgType = "create_alert"
	MsgAlertCreated MsgType = "alert_created"
	MsgCredit          MsgType = "credit"          // TUI→server: grant N bulk credits
	MsgScreenRegistry  MsgType = "screen_registry" // server→client: available plugin screens
	MsgPluginInput     MsgType = "plugin_input"    // TUI→server: plugin cell input event
)

// WireMsg is the envelope for all messages between server and client.
type WireMsg struct {
	Type       MsgType         `json:"type"`
	Topic      string          `json:"topic,omitempty"`
	Patterns   []string        `json:"patterns,omitempty"`   // for subscribe
	Payload    json.RawMessage `json:"payload,omitempty"`
	Token      string          `json:"token,omitempty"`      // for pair
	ClientName string          `json:"client_name,omitempty"` // for pair
	SessionID  string          `json:"session_id,omitempty"` // pair_ok response
	Error      string          `json:"error,omitempty"`      // error messages
	Credits    int             `json:"credits,omitempty"`    // for credit messages
	Query      string          `json:"query,omitempty"`      // for query: bucket/prefix or search term
	Limit      int             `json:"limit,omitempty"`      // for query: max results
}

// Encode serializes a WireMsg to JSON bytes.
func (m *WireMsg) Encode() ([]byte, error) {
	return json.Marshal(m)
}

// DecodeWireMsg parses a JSON frame into a WireMsg.
func DecodeWireMsg(data []byte) (*WireMsg, error) {
	var msg WireMsg
	err := json.Unmarshal(data, &msg)
	return &msg, err
}
