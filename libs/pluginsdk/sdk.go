// Package pluginsdk provides helpers for writing notbbg plugins.
//
// A plugin is a standalone binary that communicates with the notbbg server
// via JSON messages over stdin (input) and stdout (output). Plugins receive
// bus messages matching their input_topics and can publish screen updates
// or other messages to the server.
package pluginsdk

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

// StyledLine is a single line of styled text for screen rendering.
type StyledLine struct {
	Text  string `json:"text"`
	Style string `json:"style"` // "header", "normal", "green", "red", "dim", "warn"
}

// ScreenUpdate is the payload a plugin publishes for a screen.
type ScreenUpdate struct {
	ScreenID string      `json:"screen_id"`
	Lines    []StyledLine `json:"lines"`
}

// Message is the bus message format used for plugin communication.
type Message struct {
	Topic   string          `json:"Topic"`
	Payload json.RawMessage `json:"Payload"`
}

// Plugin provides helpers for reading input messages and writing screen updates.
type Plugin struct {
	screenTopic string
	enc         *json.Encoder
	scanner     *bufio.Scanner
}

// New creates a plugin that reads from stdin and writes to stdout.
// screenTopic is the topic prefix for screen updates (e.g. "plugin.my-plugin.screen").
func New(screenTopic string) *Plugin {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &Plugin{
		screenTopic: screenTopic,
		enc:         json.NewEncoder(os.Stdout),
		scanner:     scanner,
	}
}

// Read blocks until the next message from the server arrives on stdin.
func (p *Plugin) Read() (Message, error) {
	if !p.scanner.Scan() {
		if err := p.scanner.Err(); err != nil {
			return Message{}, fmt.Errorf("read stdin: %w", err)
		}
		return Message{}, fmt.Errorf("stdin closed")
	}
	var msg Message
	if err := json.Unmarshal(p.scanner.Bytes(), &msg); err != nil {
		return Message{}, fmt.Errorf("parse message: %w", err)
	}
	return msg, nil
}

// UpdateScreen publishes a screen update to stdout.
func (p *Plugin) UpdateScreen(screenID string, lines []StyledLine) {
	payload, _ := json.Marshal(ScreenUpdate{ScreenID: screenID, Lines: lines})
	p.enc.Encode(map[string]any{
		"Topic":   p.screenTopic,
		"Payload": json.RawMessage(payload),
	})
}

// Publish sends a raw message to stdout for the server to publish on the bus.
func (p *Plugin) Publish(topic string, payload any) {
	p.enc.Encode(map[string]any{
		"Topic":   topic,
		"Payload": payload,
	})
}

// Run enters the main loop: calls handler for each incoming message.
// Returns when stdin is closed or an error occurs.
func (p *Plugin) Run(handler func(msg Message)) {
	for {
		msg, err := p.Read()
		if err != nil {
			return
		}
		handler(msg)
	}
}

// ParsePayload unmarshals a Message's Payload into the given target.
func ParsePayload(msg Message, target any) error {
	return json.Unmarshal(msg.Payload, target)
}
