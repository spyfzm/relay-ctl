# relay-ctl

Console utility to control a serial (COM-port) relay board on Linux.

## Build

Requires Go 1.21+. Cross-compile static Linux binaries from any platform:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o relay-ctl-linux-amd64 .
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o relay-ctl-linux-arm64 .
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -o relay-ctl-linux-armv7 .
```

Binaries are statically linked (no glibc/musl dependency) and have no runtime
dependencies — copy one to any Linux machine of matching architecture and run it.

## Usage

```
relay-ctl -ch=<channels> -do=<action> [-dev=<device>] [-speed=<baud>]
```

- `-ch` (required): a channel number `1-255`, a comma-separated list
  (`1,2,5`), or `0`/`all` (case-insensitive) for every relay.
- `-do` (required): `on`/`1`, `off`/`0` (case-insensitive), or `read` to
  query the current state of all channels.
- `-dev` (optional): serial device, e.g. `/dev/ttyUSB0`. If omitted, the tool
  tries to auto-detect a single usable port (see below).
- `-speed` (optional): baud rate, default `9600`.

### Examples

```sh
relay-ctl -ch=5 -do=on              # turn relay 5 on
relay-ctl -ch=1,2,3 -do=off         # turn relays 1, 2, 3 off, one at a time
relay-ctl -ch=all -do=on            # turn every relay on
relay-ctl -ch=0 -do=read            # print state of all channels
relay-ctl -ch=3,7 -do=read -dev=/dev/ttyUSB0 -speed=19200
```

### Port auto-detection

When `-dev` is not given, relay-ctl tries, in order:

1. If there is exactly one free USB-serial port (e.g. a CH340/CP210x/FTDI
   adapter), use it.
2. Otherwise, if there is exactly one built-in (non-USB) serial port, use it.
3. Otherwise, print all detected ports (with USB/built-in kind, free/busy
   status, and VID:PID when available) and exit, asking you to re-run with
   `-dev=<port>` explicitly.

### Protocol

- `read`: sends byte `0xFF`; the device replies with one `CHn: ON/OFF` line
  per channel. Only the requested channels are printed.
- `on`/`off` for a single channel or `all`: sends a 4-byte command
  `A0 <channel> <0|1> <checksum>`, where `channel` is `0x00` for "all", and
  `checksum` is the sum of the first three bytes mod 256. Waits for the
  device's response before moving on.
- A comma-separated channel list is processed sequentially: each command is
  sent and its response awaited before the next is sent. The port is closed
  only after the last response is received.

Each command has an overall 2000ms timeout. If no response is received in
time, an error is printed and the tool exits with a non-zero code.

### Exit codes

| Code | Meaning                                              |
|------|-------------------------------------------------------|
| 0    | Success                                                |
| 1    | Invalid arguments                                      |
| 2    | Could not select a serial port automatically           |
| 3    | Failed to open the serial port                         |
| 4    | Command timed out waiting for a device response        |
| 5    | I/O error writing to or reading from the port           |
