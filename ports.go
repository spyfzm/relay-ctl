package main

import (
	"fmt"
	"os"

	"go.bug.st/serial/enumerator"
)

type portCandidate struct {
	Name    string
	IsUSB   bool
	VID     string
	PID     string
	Product string
	Free    bool
}

func listPorts() ([]portCandidate, error) {
	details, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, fmt.Errorf("failed to enumerate serial ports: %w", err)
	}
	out := make([]portCandidate, 0, len(details))
	for _, d := range details {
		out = append(out, portCandidate{
			Name:    d.Name,
			IsUSB:   d.IsUSB,
			VID:     d.VID,
			PID:     d.PID,
			Product: d.Product,
			Free:    isPortFree(d.Name),
		})
	}
	return out, nil
}

// autoSelectPort tries to find a single unambiguous serial port to use:
//  1. exactly one free USB-serial port (e.g. a CH340 adapter), or
//  2. exactly one built-in (non-USB) serial port.
// Otherwise it prints all detected ports and returns an error.
func autoSelectPort() (string, error) {
	ports, err := listPorts()
	if err != nil {
		return "", err
	}
	if len(ports) == 0 {
		return "", fmt.Errorf("no serial ports found on this system; connect the relay board or specify -dev explicitly")
	}

	var usbCandidates, builtinCandidates []portCandidate
	for _, p := range ports {
		if p.IsUSB {
			usbCandidates = append(usbCandidates, p)
		} else {
			builtinCandidates = append(builtinCandidates, p)
		}
	}

	if len(usbCandidates) == 1 && usbCandidates[0].Free {
		return usbCandidates[0].Name, nil
	}

	if len(builtinCandidates) == 1 {
		return builtinCandidates[0].Name, nil
	}

	printPortList(ports)
	return "", fmt.Errorf("could not determine a single serial port automatically; re-run with -dev=<port> to select one")
}

func printPortList(ports []portCandidate) {
	fmt.Fprintln(os.Stderr, "Could not auto-select a serial port. Available ports:")
	for _, p := range ports {
		kind := "built-in"
		if p.IsUSB {
			kind = "USB"
		}
		status := "free"
		if !p.Free {
			status = "busy"
		}
		desc := p.Product
		if p.VID != "" || p.PID != "" {
			if desc != "" {
				desc += " "
			}
			desc += fmt.Sprintf("(VID:PID=%s:%s)", p.VID, p.PID)
		}
		fmt.Fprintf(os.Stderr, "  %-20s %-8s %-6s %s\n", p.Name, kind, status, desc)
	}
	fmt.Fprintln(os.Stderr, "Re-run with -dev=<port> to select one explicitly.")
}
