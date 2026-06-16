package main

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"go.bug.st/serial"
)

func TestParseStrict(t *testing.T) {
	for _, s := range []string{"yes", "Yes", "YES", "y", "true", "1", " yes "} {
		got, err := parseStrict(s)
		if err != nil || !got {
			t.Errorf("parseStrict(%q) = %v, %v; want true, nil", s, got, err)
		}
	}
	for _, s := range []string{"", "no", "No", "n", "false", "0", "  no  "} {
		got, err := parseStrict(s)
		if err != nil || got {
			t.Errorf("parseStrict(%q) = %v, %v; want false, nil", s, got, err)
		}
	}
	for _, s := range []string{"maybe", "2", "onn"} {
		if _, err := parseStrict(s); err == nil {
			t.Errorf("parseStrict(%q) expected error, got nil", s)
		}
	}
}

func TestPartitionChannels(t *testing.T) {
	existing := map[int]bool{1: true, 2: true, 5: true}
	tests := []struct {
		name             string
		requested        []int
		present, missing []int
	}{
		{"some missing, unsorted input", []int{5, 3, 1, 7}, []int{1, 5}, []int{3, 7}},
		{"all present", []int{2, 1}, []int{1, 2}, nil},
		{"all missing", []int{9, 8}, nil, []int{8, 9}},
		{"single present", []int{5}, []int{5}, nil},
		{"single missing", []int{4}, nil, []int{4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			present, missing := partitionChannels(tt.requested, existing)
			if !reflect.DeepEqual(present, tt.present) {
				t.Errorf("present = %v; want %v", present, tt.present)
			}
			if !reflect.DeepEqual(missing, tt.missing) {
				t.Errorf("missing = %v; want %v", missing, tt.missing)
			}
		})
	}
}

func TestPartitionChannelsDoesNotMutateInput(t *testing.T) {
	requested := []int{5, 3, 1}
	partitionChannels(requested, map[int]bool{1: true})
	if !reflect.DeepEqual(requested, []int{5, 3, 1}) {
		t.Errorf("input was mutated: %v", requested)
	}
}

func TestJoinInts(t *testing.T) {
	if got := joinInts([]int{3, 7, 12}); got != "3, 7, 12" {
		t.Errorf("joinInts = %q; want %q", got, "3, 7, 12")
	}
	if got := joinInts([]int{4}); got != "4" {
		t.Errorf("joinInts single = %q; want %q", got, "4")
	}
	if got := joinInts(nil); got != "" {
		t.Errorf("joinInts(nil) = %q; want empty", got)
	}
}

func TestParseChannelStatesExtractsExisting(t *testing.T) {
	// A relay board's read reply lists one line per existing channel; the
	// write pre-check relies on this to know which relays are present.
	reply := "CH1: ON\r\nCH2: OFF\r\nCH5: 1\r\n"
	states := parseChannelStates(reply)
	got := map[int]string{}
	for _, st := range states {
		got[st.channel] = st.state
	}
	want := map[int]string{1: "ON", 2: "OFF", 5: "ON"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseChannelStates = %v; want %v", got, want)
	}
}

// fakePort is a minimal serial.Port for exercising the write flow. It answers
// the 0xFF probe with listing and each A0 command with ack, and records every
// payload passed to Write.
type fakePort struct {
	listing  string   // reply to the 0xFF read/probe command
	ack      string   // reply to each A0 write command
	failRead bool     // when true, Read returns an I/O error
	pending  []byte   // bytes queued to be handed out by Read
	writes   [][]byte // payloads passed to Write, in order
}

func (f *fakePort) Write(p []byte) (int, error) {
	cmd := append([]byte(nil), p...)
	f.writes = append(f.writes, cmd)
	switch {
	case len(cmd) == 1 && cmd[0] == 0xFF:
		f.pending = append(f.pending, []byte(f.listing)...)
	case len(cmd) > 0 && cmd[0] == 0xA0:
		f.pending = append(f.pending, []byte(f.ack)...)
	}
	return len(p), nil
}

func (f *fakePort) Read(p []byte) (int, error) {
	if f.failRead {
		return 0, errors.New("simulated read failure")
	}
	if len(f.pending) == 0 {
		return 0, nil
	}
	n := copy(p, f.pending)
	f.pending = f.pending[n:]
	return n, nil
}

func (f *fakePort) SetReadTimeout(time.Duration) error { return nil }
func (f *fakePort) Close() error                       { return nil }
func (f *fakePort) SetMode(*serial.Mode) error         { return nil }
func (f *fakePort) Drain() error                       { return nil }
func (f *fakePort) ResetInputBuffer() error            { return nil }
func (f *fakePort) ResetOutputBuffer() error           { return nil }
func (f *fakePort) SetDTR(bool) error                  { return nil }
func (f *fakePort) SetRTS(bool) error                  { return nil }
func (f *fakePort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (f *fakePort) Break(time.Duration) error { return nil }

// commandedChannels returns the channel byte of every A0 write, in order.
func (f *fakePort) commandedChannels() []int {
	var chans []int
	for _, w := range f.writes {
		if len(w) >= 2 && w[0] == 0xA0 {
			chans = append(chans, int(w[1]))
		}
	}
	return chans
}

func (f *fakePort) probedFirst() bool {
	return len(f.writes) >= 1 && len(f.writes[0]) == 1 && f.writes[0][0] == 0xFF
}

func TestDoWriteAllProbesThenSends(t *testing.T) {
	f := &fakePort{listing: "CH1: OFF\r\nCH2: OFF\r\n", ack: "ok\r\n"}
	if code := doWrite(f, true, nil, true, false); code != exitOK {
		t.Fatalf("code = %d; want %d", code, exitOK)
	}
	if !f.probedFirst() {
		t.Fatalf("expected first write to be the 0xFF probe, got %v", f.writes)
	}
	if got := f.commandedChannels(); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("commanded channels = %v; want [0] (all)", got)
	}
}

func TestDoWriteAllModuleUnavailableNoListing(t *testing.T) {
	f := &fakePort{listing: "garbage, no channels here\r\n", ack: "ok\r\n"}
	if code := doWrite(f, true, nil, true, false); code != exitIOError {
		t.Fatalf("code = %d; want %d", code, exitIOError)
	}
	if got := f.commandedChannels(); got != nil {
		t.Errorf("no A0 command should be sent when module is unavailable; got %v", got)
	}
}

func TestDoWriteModuleUnavailableReadError(t *testing.T) {
	f := &fakePort{failRead: true}
	if code := doWrite(f, true, nil, true, false); code != exitIOError {
		t.Fatalf("code = %d; want %d", code, exitIOError)
	}
	if got := f.commandedChannels(); got != nil {
		t.Errorf("no A0 command should be sent on read failure; got %v", got)
	}
}

func TestDoWriteSpecificSkipsMissingNonStrict(t *testing.T) {
	f := &fakePort{listing: "CH1: OFF\r\nCH2: OFF\r\nCH3: OFF\r\n", ack: "ok\r\n"}
	if code := doWrite(f, false, []int{2, 9}, true, false); code != exitOK {
		t.Fatalf("code = %d; want %d", code, exitOK)
	}
	if got := f.commandedChannels(); !reflect.DeepEqual(got, []int{2}) {
		t.Errorf("commanded channels = %v; want [2] (9 absent, skipped)", got)
	}
}

func TestDoWriteStrictMissingSendsNothing(t *testing.T) {
	f := &fakePort{listing: "CH1: OFF\r\nCH2: OFF\r\n", ack: "ok\r\n"}
	if code := doWrite(f, false, []int{2, 9}, true, true); code != exitMissingChannels {
		t.Fatalf("code = %d; want %d", code, exitMissingChannels)
	}
	if got := f.commandedChannels(); got != nil {
		t.Errorf("strict mode must send no A0 commands; got %v", got)
	}
	if !f.probedFirst() || len(f.writes) != 1 {
		t.Errorf("expected only the 0xFF probe to be written, got %v", f.writes)
	}
}

func TestDoWriteSpecificAllMissing(t *testing.T) {
	f := &fakePort{listing: "CH1: OFF\r\n", ack: "ok\r\n"}
	if code := doWrite(f, false, []int{8, 9}, true, false); code != exitMissingChannels {
		t.Fatalf("code = %d; want %d", code, exitMissingChannels)
	}
	if got := f.commandedChannels(); got != nil {
		t.Errorf("no A0 command should be sent when all requested channels are absent; got %v", got)
	}
}
