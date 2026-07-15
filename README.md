# gocamdet

Detect whether a USB camera is currently in use on a Linux system, and see
which process is using it. Pure Go, no cgo, no libusb, no external tools.

Disclaimer: This works for me — that's the entire guarantee. Built with AI in the loop, so check your own biases before you love it or hate it on principle. Use at your own risk, fork freely, and don't @ me when it explodes. (But do drop me a note if it helps — pay it forward.)

## What it does

gocamdet reads the Linux kernel's own interfaces to answer "is my webcam on,
and who turned it on?":

- Enumerates video devices from `/sys/class/video4linux`.
- Keeps only cameras whose sysfs path traces to a **USB** bus.
- Groups the multiple `/dev/videoN` nodes a single physical camera exposes
  (capture + metadata) into one entry.
- Marks a camera **in use** when any process holds one of its nodes open,
  found by scanning `/proc/*/fd`, and reports the owning process.

Linux only. See [`VISION.md`](VISION.md) for full scope and non-goals.

## Install

```sh
go install github.com/gherlein/gocamdet/cmd/gocamdet@latest
```

Or build from source:

```sh
make build      # produces ./bin/gocamdet
```

## CLI usage

```sh
gocamdet          # human-readable table
gocamdet --json   # machine-readable JSON on stdout
```

Exit codes (for scripting):

| Code | Meaning |
| --- | --- |
| 0 | No USB camera in use |
| 1 | At least one USB camera in use |
| 2 | Error |

Example:

```sh
if gocamdet >/dev/null; then
  echo "camera is free"
else
  echo "camera is in use"
fi
```

### Permissions

Reading another user's `/proc/<pid>/fd` requires root. Without it, gocamdet
still reports every camera and detects handles held by **your own** processes,
but it prints a note to stderr and sets `fullVisibility: false` in JSON. Run
with `sudo` for complete process attribution:

```sh
sudo gocamdet
```

## Library usage

```go
package main

import (
	"fmt"

	"github.com/gherlein/gocamdet"
)

func main() {
	result, err := camdet.Detect()
	if err != nil {
		panic(err)
	}
	for _, cam := range result.Cameras {
		fmt.Printf("%s (%s:%s) in use: %t\n", cam.Name, cam.VendorID, cam.ProductID, cam.InUse)
		for _, u := range cam.Users {
			fmt.Printf("  used by %s (pid %d) via %s\n", u.Name, u.PID, u.Node)
		}
	}
	if !result.FullVisibility {
		fmt.Println("note: run as root for complete process attribution")
	}
}
```

## Development

```sh
make test    # run the test suite (synthetic sysfs/proc fixtures; no hardware or root needed)
make vet     # go vet
make lint    # golangci-lint if installed
make build   # build the CLI
```

The core scan is parameterized by a filesystem root, so the entire detection
pipeline is tested against synthetic sysfs/proc trees without touching real
hardware or requiring elevated privileges.
