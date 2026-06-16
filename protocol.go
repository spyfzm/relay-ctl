package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
)

// buildCommand builds the 4-byte relay command. channel 0 addresses all relays.
func buildCommand(channel int, on bool) []byte {
	var action byte
	if on {
		action = 1
	}
	sum := (0xA0 + int(byte(channel)) + int(action)) % 256
	return []byte{0xA0, byte(channel), action, byte(sum)}
}

const (
	readPollInterval = 20 * time.Millisecond
	readIdleGap      = 150 * time.Millisecond
)

// readResponse reads from port until the device goes idle (no new bytes for
// readIdleGap after at least one byte arrived) or the overall timeout
// elapses. It returns an error if no data was received at all.
func readResponse(port serial.Port, timeout time.Duration) (string, error) {
	if err := port.SetReadTimeout(readPollInterval); err != nil {
		return "", fmt.Errorf("failed to set read timeout: %w", err)
	}

	deadline := time.Now().Add(timeout)
	var buf []byte
	var lastDataAt time.Time

	for time.Now().Before(deadline) {
		chunk := make([]byte, 256)
		n, err := port.Read(chunk)
		if err != nil {
			return "", fmt.Errorf("read error: %w", err)
		}
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			lastDataAt = time.Now()
			continue
		}
		if !lastDataAt.IsZero() && time.Since(lastDataAt) >= readIdleGap {
			break
		}
	}

	if len(buf) == 0 {
		return "", fmt.Errorf("timeout: no response received from device within %s", timeout)
	}
	return strings.TrimRight(string(buf), "\r\n"), nil
}

type channelState struct {
	channel int
	state   string
}

var channelLineRE = regexp.MustCompile(`(?i)CH\s*(\d+)\s*:\s*(\S+)`)

// parseChannelStates extracts "CHn: <state>" lines from raw device output,
// normalizing 0/1/off/on to OFF/ON. Lines that don't match are ignored.
func parseChannelStates(text string) []channelState {
	matches := channelLineRE.FindAllStringSubmatch(text, -1)
	states := make([]channelState, 0, len(matches))
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		states = append(states, channelState{channel: n, state: normalizeState(m[2])})
	}
	return states
}

// readChannelStates sends the read command (0xFF) and returns the channel
// states reported by the device. It is used both by the read action and to
// validate requested channels before a write.
func readChannelStates(port serial.Port) ([]channelState, error) {
	if _, err := port.Write([]byte{0xFF}); err != nil {
		return nil, fmt.Errorf("failed to write read command: %w", err)
	}
	text, err := readResponse(port, commandTimeout)
	if err != nil {
		return nil, err
	}
	states := parseChannelStates(text)
	if len(states) == 0 {
		return nil, fmt.Errorf("no recognizable channel data in device response")
	}
	return states, nil
}

func normalizeState(s string) string {
	switch strings.ToLower(s) {
	case "0", "off":
		return "OFF"
	case "1", "on":
		return "ON"
	default:
		return strings.ToUpper(s)
	}
}
