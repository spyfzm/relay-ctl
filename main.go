// relay-ctl controls LCUS-series serial relay modules (made by Shenzhen LC
// Technology Co.,Ltd) over a COM port from the command line.
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
	exitOK              = 0
	exitInvalidArgs     = 1
	exitPortSelection   = 2
	exitPortOpenError   = 3
	exitCommandTimeout  = 4
	exitIOError         = 5
	exitMissingChannels = 6
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
	strictFlag := flag.String("strict", "no", "strict mode (yes/no): with yes, abort without sending anything if any requested channel is absent")
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

	strict, err := parseStrict(*strictFlag)
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
	return doWrite(port, all, channels, act == actionOn, strict)
}

func printUsage() {
	fmt.Fprint(os.Stderr, `relay-ctl - control an LCUS serial relay module (Shenzhen LC Technology) over a COM port

Usage:
  relay-ctl -ch=<channels> -do=<action> [-dev=<device>] [-speed=<baud>] [-strict=<yes|no>]

Flags:
  -ch     required. Channel(s) to control: a number 1-255, a comma-separated
          list (e.g. "1,2,5"), or "0"/"all" for every relay.
  -do     required. Action to perform: "on"/"1", "off"/"0" (case-insensitive),
          or "read" to query the current state of all channels.
  -dev    optional. Serial device to use, e.g. /dev/ttyUSB0. If omitted, the
          tool tries to auto-detect a single suitable port.
  -speed  optional. Serial baud rate (default 9600).
  -strict optional (default "no"). Only affects writes to specific channels:
          the device is first queried (read) to see which relays exist.
          With "no", commands are sent only to channels that exist and absent
          ones are reported as a warning. With "yes", if any requested channel
          is absent, no commands are sent and the tool exits with an error.

Examples:
  relay-ctl -ch=5 -do=on
  relay-ctl -ch=1,2,3 -do=off
  relay-ctl -ch=all -do=on
  relay-ctl -ch=3,7 -do=on -strict=yes
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

func parseStrict(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "no", "n", "false", "0":
		return false, nil
	case "yes", "y", "true", "1":
		return true, nil
	default:
		return false, fmt.Errorf("invalid -strict value %q: expected yes or no", s)
	}
}

func doRead(port serial.Port, all bool, channels []int) int {
	states, err := readChannelStates(port)
	if err != nil {
		return commandErrorCode(err)
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

func doWrite(port serial.Port, all bool, channels []int, on bool, strict bool) int {
	// Confirm the module is present before sending anything: send 0xFF and
	// require a channel listing in the reply. This guards every write —
	// including "all" — and, for specific channels, also tells us which relays
	// actually exist.
	states, err := readChannelStates(port)
	if err != nil {
		return moduleUnavailable(err)
	}

	if all {
		text, err := sendAndWait(port, buildCommand(0, on))
		if err != nil {
			return commandErrorCode(err)
		}
		fmt.Println(text)
		return exitOK
	}

	existing := make(map[int]bool, len(states))
	for _, st := range states {
		existing[st.channel] = true
	}

	present, missing := partitionChannels(channels, existing)

	if len(missing) > 0 {
		if strict {
			fmt.Fprintf(os.Stderr, "error: channel(s) not present on device: %s\n", joinInts(missing))
			fmt.Fprintln(os.Stderr, "strict mode: no commands sent")
			return exitMissingChannels
		}
		fmt.Fprintf(os.Stderr, "warning: skipping channel(s) not present on device: %s\n", joinInts(missing))
	}

	if len(present) == 0 {
		fmt.Fprintln(os.Stderr, "error: none of the requested channels are present on the device")
		return exitMissingChannels
	}

	for _, ch := range present {
		text, err := sendAndWait(port, buildCommand(ch, on))
		if err != nil {
			return commandErrorCode(err)
		}
		fmt.Println(text)
	}
	return exitOK
}

// partitionChannels splits requested channels into those present on the device
// and those absent, each returned in ascending order.
func partitionChannels(requested []int, existing map[int]bool) (present, missing []int) {
	sorted := append([]int{}, requested...)
	sort.Ints(sorted)
	for _, ch := range sorted {
		if existing[ch] {
			present = append(present, ch)
		} else {
			missing = append(missing, ch)
		}
	}
	return present, missing
}

func joinInts(nums []int) string {
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

// commandErrorCode prints err and maps a failed device exchange to the right
// exit code (timeout vs. generic I/O).
func commandErrorCode(err error) int {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	if strings.HasPrefix(err.Error(), "timeout") {
		return exitCommandTimeout
	}
	return exitIOError
}

// moduleUnavailable reports a failed pre-write health check: the module did not
// return a usable channel listing in response to the 0xFF query. The exit code
// mirrors commandErrorCode (timeout vs. generic I/O / unrecognized response).
func moduleUnavailable(err error) int {
	fmt.Fprintf(os.Stderr, "error: relay module unavailable: %v\n", err)
	if strings.HasPrefix(err.Error(), "timeout") {
		return exitCommandTimeout
	}
	return exitIOError
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
