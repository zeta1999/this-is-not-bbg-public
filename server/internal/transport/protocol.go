// Package transport defines the JSON wire protocol between server and clients.
// This is a simple framing protocol used until protobuf codegen is wired up.
package transport

import "encoding/json"

// ProtocolMajor / ProtocolMinor — version of the wire protocol.
//
// Bump rules (U8, 2026-04-25):
//   - Major: any breaking change to the WireMsg envelope, an existing
//     field's semantics, or removal of a message type. Major mismatch
//     between client and server REFUSES the connection.
//   - Minor: backward-compatible additions (new MsgType, new optional
//     field). Minor mismatch is accepted; the older side logs INFO
//     and ignores anything it doesn't recognise.
//
// Default-1 contract: a client that doesn't send ClientHello is
// treated as {major:1, minor:0} so pre-U8 clients keep working.
const (
	ProtocolMajor uint32 = 1
	ProtocolMinor uint32 = 0
)

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
	MsgOverflow        MsgType = "overflow"        // server→client: N messages dropped since last notice
	MsgPluginJobCancel MsgType = "plugin_job_cancel" // client→server: cancel a long-running plugin job
	MsgClientHello     MsgType = "client_hello"      // client→server: protocol version + name (U8)
	MsgServerHello     MsgType = "server_hello"      // server→client: protocol version (U8)
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
	Dropped    uint32          `json:"dropped,omitempty"`    // for overflow: messages dropped since last notice
	JobID      string          `json:"job_id,omitempty"`     // for plugin_job_cancel
	Major      uint32          `json:"major,omitempty"`      // for client_hello / server_hello (U8)
	Minor      uint32          `json:"minor,omitempty"`      // for client_hello / server_hello (U8)
}

// VersionMismatch reports whether a peer's announced (major, minor)
// would refuse the handshake. Returns ("", false, false) when the
// versions match exactly. Returns ("", true, true) when only the
// minor disagrees (continue + INFO log on the older side). Returns
// (reason, true, false) when major disagrees (refuse).
func VersionMismatch(peerMajor, peerMinor uint32) (reason string, mismatched bool, accept bool) {
	if peerMajor != ProtocolMajor {
		return "protocol major mismatch: peer=" + uintToStr(peerMajor) +
			" server=" + uintToStr(ProtocolMajor), true, false
	}
	if peerMinor != ProtocolMinor {
		return "protocol minor differs: peer=" + uintToStr(peerMinor) +
			" server=" + uintToStr(ProtocolMinor), true, true
	}
	return "", false, true
}

func uintToStr(v uint32) string {
	if v == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
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
