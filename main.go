// relay-ctl controls a serial relay board (COM port) from the command line.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
)

// Exit codes.
const (
	exitOK             = 0
	exitInvalidArgs    = 1
	exitPortSelection  = 2
	exitPortOpenError  = 3
	exitCommandTimeout = 4
	exitIOError        = 5
)

const commandTimeout = 2000 * time.Millisecond

type action int

const (
	actionOff action = iota
	actionOn
	actionRead
)

func main() {
	os.Exit(run())
}

func run() int {
	chFlag := flag.String("ch", "", "channel(s) to control: 1-255, a comma-separated list (e.g. 1,2,5), or 0/all for every relay")
	doFlag := flag.String("do", "", "action: on, off, 1, 0 (case-insensitive), or read")
	devFlag := flag.String("dev", "", "serial device to use (e.g. /dev/ttyUSB0); auto-detected if omitted")
	speedFlag := flag.Int("speed", 9600, "serial port baud rate")
	flag.Usage = printUsage
	flag.Parse()

	if *chFlag == "" || *doFlag == "" {
		fmt.Fprintln(os.Stderr, "error: -ch and -do are required")
		printUsage()
		return exitInvalidArgs
	}

	all, channels, err := parseChannels(*chFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitInvalidArgs
	}

	act, err := parseAction(*doFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitInvalidArgs
	}

	if *speedFlag <= 0 {
		fmt.Fprintf(os.Stderr, "error: invalid -speed value %d\n", *speedFlag)
		return exitInvalidArgs
	}

	devName := *devFlag
	if devName == "" {
		devName, err = autoSelectPort()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return exitPortSelection
		}
		fmt.Fprintf(os.Stderr, "using auto-detected port: %s\n", devName)
	}

	mode := &serial.Mode{
		BaudRate: *speedFlag,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	port, err := serial.Open(devName, mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to open port %s: %v\n", devName, err)
		return exitPortOpenError
	}
	defer port.Close()

	if act == actionRead {
		return doRead(port, all, channels)
	}
	return doWrite(port, all, channels, act == actionOn)
}

func printUsage() {
	fmt.Fprint(os.Stderr, `relay-ctl - control a serial relay board over a COM port

Usage:
  relay-ctl -ch=<channels> -do=<action> [-dev=<device>] [-speed=<baud>]

Flags:
  -ch     required. Channel(s) to control: a number 1-255, a comma-separated
          list (e.g. "1,2,5"), or "0"/"all" for every relay.
  -do     required. Action to perform: "on"/"1", "off"/"0" (case-insensitive),
          or "read" to query the current state of all channels.
  -dev    optional. Serial device to use, e.g. /dev/ttyUSB0. If omitted, the
          tool tries to auto-detect a single suitable port.
  -speed  optional. Serial baud rate (default 9600).

Examples:
  relay-ctl -ch=5 -do=on
  relay-ctl -ch=1,2,3 -do=off
  relay-ctl -ch=all -do=on
  relay-ctl -ch=0 -do=read -dev=/dev/ttyUSB0
`)
}

func parseChannels(s string) (all bool, channels []int, err error) {
	s = strings.TrimSpace(s)
	if s == "0" || strings.EqualFold(s, "all") {
		return true, nil, nil
	}

	seen := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, convErr := strconv.Atoi(part)
		if convErr != nil {
			return false, nil, fmt.Errorf("invalid channel %q: not a number", part)
		}
		if n == 256 {
			return false, nil, fmt.Errorf("channel 256 is out of range: the protocol encodes the channel number in a single byte (1-255), so channel 256 cannot be addressed individually")
		}
		if n < 1 || n > 255 {
			return false, nil, fmt.Errorf("channel %d is out of range (must be 1-255, or 0/all)", n)
		}
		if !seen[n] {
			seen[n] = true
			channels = append(channels, n)
		}
	}
	if len(channels) == 0 {
		return false, nil, fmt.Errorf("no valid channels specified in %q", s)
	}
	return false, channels, nil
}

func parseAction(s string) (action, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "0", "off":
		return actionOff, nil
	case "1", "on":
		return actionOn, nil
	case "read":
		return actionRead, nil
	default:
		return 0, fmt.Errorf("invalid -do value %q: expected one of 0, 1, off, on, read", s)
	}
}

func doRead(port serial.Port, all bool, channels []int) int {
	if _, err := port.Write([]byte{0xFF}); err != nil {
		fmt.Fprintf(os.Stderr, "error: failed to write read command: %v\n", err)
		return exitIOError
	}

	text, err := readResponse(port, commandTimeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitCommandTimeout
	}

	states := parseChannelStates(text)
	if len(states) == 0 {
		fmt.Fprintln(os.Stderr, "error: no recognizable channel data in device response")
		return exitIOError
	}

	wanted := map[int]bool{}
	if !all {
		for _, c := range channels {
			wanted[c] = true
		}
	}

	for _, st := range states {
		if all || wanted[st.channel] {
			fmt.Printf("CH%d: %s\n", st.channel, st.state)
		}
	}
	return exitOK
}

func doWrite(port serial.Port, all bool, channels []int, on bool) int {
	if all {
		cmd := buildCommand(0, on)
		text, err := sendAndWait(port, cmd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			if strings.HasPrefix(err.Error(), "timeout") {
				return exitCommandTimeout
			}
			return exitIOError
		}
		fmt.Println(text)
		return exitOK
	}

	sortedChannels := append([]int{}, channels...)
	sort.Ints(sortedChannels)

	for _, ch := range sortedChannels {
		cmd := buildCommand(ch, on)
		text, err := sendAndWait(port, cmd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			if strings.HasPrefix(err.Error(), "timeout") {
				return exitCommandTimeout
			}
			return exitIOError
		}
		fmt.Println(text)
	}
	return exitOK
}

func sendAndWait(port serial.Port, cmd []byte) (string, error) {
	if _, err := port.Write(cmd); err != nil {
		return "", fmt.Errorf("failed to write command: %w", err)
	}
	text, err := readResponse(port, commandTimeout)
	if err != nil {
		return "", err
	}
	return text, nil
}
