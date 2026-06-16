package main

import (
	"reflect"
	"testing"
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
