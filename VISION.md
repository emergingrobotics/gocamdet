# gocamdet — Vision

## One-liner

A Go module (and an example CLI) that detects whether a USB camera is currently
in use on a Linux system, and reports which process is using it.

## Problem

On Linux there is no single, obvious answer to "is my webcam on right now, and
who turned it on?" The information exists in the kernel — video devices under
`/sys/class/video4linux`, USB topology under `/sys/bus/usb`, and open file
handles under `/proc/*/fd` — but stitching it together requires knowledge of
V4L2, sysfs, and `/proc` semantics. gocamdet packages that knowledge behind a
small, well-defined Go API and a scriptable CLI.

## Scope

- **Platform:** Linux only. Detection relies on the Linux kernel's V4L2, sysfs,
  and procfs interfaces. The public API is kept backend-agnostic so other
  platforms could be added later, but no non-Linux code is in scope now.
- **Devices:** USB cameras only. A `/dev/videoN` node is included only when its
  sysfs path traces to a USB bus. Built-in/MIPI, PCI, and virtual cameras
  (e.g. v4l2loopback, OBS) are excluded.
- **"In use" definition:** a camera is in use when any process holds one of its
  `/dev/videoN` nodes open. This is detected by scanning `/proc/*/fd`. We do not
  attempt to distinguish "open" from "actively streaming" — that is unreliable
  on Linux while another process owns the device.
- **Grouping:** modern UVC cameras expose several `/dev/videoN` nodes per
  physical camera (capture + metadata). gocamdet reports one entry per physical
  USB device, listing all of its nodes; the camera is "in use" if any node is
  open.

## What it delivers

1. **A Go module** exposing a single `Detect()` call that returns the set of USB
   cameras, whether each is in use, and which processes are using it.
2. **An example CLI** (`cmd/gocamdet`) that prints a human-readable snapshot by
   default, supports `--json` for machine consumption, and sets a scriptable
   exit code (`0` = none in use, `1` = at least one in use, `2` = error).
3. **A continuous watch mode** (`--watch`) that polls on an interval and runs a
   hook script on aggregate on/off transitions (any camera in use vs. none),
   plus an example systemd unit to run it at boot.

## Design principles

- **Pure Go, no cgo, no libusb.** Read directly from the kernel's own source of
  truth (sysfs/procfs). Zero external runtime dependencies; trivial to
  cross-compile.
- **Graceful degradation on permissions.** Reading another user's
  `/proc/[pid]/fd` requires root (or `CAP_SYS_PTRACE`). Without it, gocamdet
  still reports what it can see and clearly flags that visibility is reduced,
  rather than failing or lying.
- **Testable without hardware or root.** Core logic is parameterized by a
  filesystem root so it can run against synthetic sysfs/proc trees in tests.
- **Explicit errors, no forgiving code.** Environment precondition violations
  (no V4L2, no procfs) are hard errors; transient conditions (a device
  unplugged mid-scan, a process exiting, an unreadable fd) are tolerated and
  surfaced as reduced visibility.

## Explicit non-goals

- No live streaming/frame-level state detection.
- No control of the camera (start/stop/kill the using process).
- No macOS/Windows implementation (yet).
- No event-based (inotify/fanotify/udev) detection; watch mode polls, keeping
  the zero-dependency, pure-Go design.
- No per-camera transition granularity; watch mode reports the aggregate
  any-camera-in-use state.
- No use of external tools (`lsof`, `fuser`, `v4l2-ctl`) at runtime.

## Success criteria

- `Detect()` correctly enumerates USB cameras, groups multi-node devices, and
  reports in-use status and the owning process (when visible).
- The CLI's exit code reliably reflects in-use status for shell scripting.
- The full test suite passes against synthetic fixtures without root or a real
  camera.
