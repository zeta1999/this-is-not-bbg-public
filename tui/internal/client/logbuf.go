package client

import (
	"bufio"
	"io"
	"sync"
)

// LogBuffer is a thread-safe ring buffer of log lines captured from the server process.
type LogBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
}

// NewLogBuffer creates a buffer that keeps the last maxLines.
func NewLogBuffer(maxLines int) *LogBuffer {
	return &LogBuffer{
		lines: make([]string, 0, maxLines),
		max:   maxLines,
	}
}

// Writer returns an io.Writer that captures lines into the buffer.
// Attach to cmd.Stdout / cmd.Stderr.
func (lb *LogBuffer) Writer() io.Writer {
	pr, pw := io.Pipe()
	go func() {
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 64*1024), 64*1024)
		for scanner.Scan() {
			lb.Append(scanner.Text())
		}
	}()
	return pw
}

// Append adds a line to the buffer.
func (lb *LogBuffer) Append(line string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.lines = append(lb.lines, line)
	if len(lb.lines) > lb.max {
		// Drop oldest.
		copy(lb.lines, lb.lines[len(lb.lines)-lb.max:])
		lb.lines = lb.lines[:lb.max]
	}
}

// Lines returns a copy of all buffered lines.
func (lb *LogBuffer) Lines() []string {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	out := make([]string, len(lb.lines))
	copy(out, lb.lines)
	return out
}

// Len returns the number of lines in the buffer.
func (lb *LogBuffer) Len() int {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	return len(lb.lines)
}
