package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"sync"
)

const logEntryLimit = 2000

type LogEntry struct {
	ID        int64  `json:"id"`
	Timestamp string `json:"timestamp"`
	Stream    string `json:"stream"`
	Message   string `json:"message"`
}

type LogBuffer struct {
	mu      sync.Mutex
	nextID  int64
	max     int
	entries []LogEntry
}

var sessionLogs = newLogBuffer(logEntryLimit)

func newLogBuffer(max int) *LogBuffer {
	if max <= 0 {
		max = logEntryLimit
	}
	return &LogBuffer{max: max}
}

func (buffer *LogBuffer) Append(stream, message string) LogEntry {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	buffer.nextID++
	entry := LogEntry{
		ID:        buffer.nextID,
		Timestamp: utcNow(),
		Stream:    stream,
		Message:   strings.TrimRight(message, "\r\n"),
	}
	buffer.entries = append(buffer.entries, entry)
	if len(buffer.entries) > buffer.max {
		overflow := len(buffer.entries) - buffer.max
		copy(buffer.entries, buffer.entries[overflow:])
		buffer.entries = buffer.entries[:buffer.max]
	}
	return entry
}

func (buffer *LogBuffer) Since(since int64) []LogEntry {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()

	start := 0
	if since > 0 {
		for start < len(buffer.entries) && buffer.entries[start].ID <= since {
			start++
		}
	}
	entries := make([]LogEntry, len(buffer.entries[start:]))
	copy(entries, buffer.entries[start:])
	return entries
}

func (buffer *LogBuffer) LatestID() int64 {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.nextID
}

func appLog(format string, args ...any) {
	sessionLogs.Append("app", fmt.Sprintf(format, args...))
}

func appendLogLine(stream, line string) {
	line = strings.TrimRight(line, "\r\n")
	if isTransientLogFrame(line) {
		return
	}
	sessionLogs.Append(stream, line)
}

func isTransientLogFrame(line string) bool {
	switch strings.TrimSpace(line) {
	case "|", "/", `\`, "-":
		return true
	default:
		return false
	}
}

func appendLogChunk(stream, pending, chunk string) string {
	text := pending + chunk
	holdCR := strings.HasSuffix(text, "\r")
	if holdCR {
		text = strings.TrimSuffix(text, "\r")
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")

	var line strings.Builder
	for _, char := range text {
		switch char {
		case '\n':
			appendLogLine(stream, line.String())
			line.Reset()
		case '\r':
			line.Reset()
		default:
			line.WriteRune(char)
		}
	}
	if holdCR {
		return line.String() + "\r"
	}
	return line.String()
}

func streamCommandOutput(reader io.Reader, stream string, output *bytes.Buffer, wg *sync.WaitGroup) {
	defer wg.Done()

	pending := ""
	buffer := make([]byte, 4096)
	for {
		n, err := reader.Read(buffer)
		if n > 0 {
			chunk := string(buffer[:n])
			output.WriteString(chunk)
			pending = appendLogChunk(stream, pending, chunk)
		}
		if err != nil {
			if err != io.EOF {
				appLog("Error reading %s stream: %s", stream, err)
			}
			break
		}
	}
	if pending != "" {
		appendLogLine(stream, strings.TrimSuffix(pending, "\r"))
	}
}
