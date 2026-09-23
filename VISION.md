# gocamdet — Vision

## One-liner

A Go module (and an example CLI) that detects whether a USB camera or a
microphone is currently in use on a Linux system, distinguishes a camera that is
merely open from one that is actively streaming, and reports which process is
using each device.

## Problem

On Linux there is no single, obvious answer to "is my webcam or mic on right
now, and who turned it on?" The information exists — video devices under
`/sys/class/video4linux`, USB topology under `/sys/bus/usb`, open file handles
under `/proc/*/fd`, and the audio graph inside PipeWire or ALSA — but stitching
it together requires knowledge of V4L2, sysfs, `/proc`, and sound-server
semantics. gocamdet packages that knowledge behind a small, well-defined Go API
and a scriptable CLI.

## Scope

- **Platform:** Linux only. Camera detection relies on the kernel's V4L2, sysfs,
  and procfs interfaces; microphone detection on PipeWire or ALSA. The public
  API is kept backend-agnostic so other platforms could be added later, but no
  non-Linux code is in scope now.
- **Cameras:** USB cameras only. A `/dev/videoN` node is included only when its
  sysfs path traces to a USB bus. Built-in/MIPI, PCI, and virtual cameras
  (e.g. v4l2loopback, OBS) are excluded.
- **Camera state — open vs. streaming:** two distinct facts are reported. A
  camera is *open* when a process holds one of its `/dev/videoN` nodes open
  (scanned from `/proc/*/fd`). A camera is *streaming* when its USB
  VideoStreaming interface selects a non-zero alternate setting to allocate
  isochronous bandwidth — the transport-level signal of active capture,
  independent of how a client maps buffers. This detects capture through
  PipeWire and the xdg camera portal as well as direct V4L2 clients. Cameras
  that stream over USB bulk endpoints cannot be distinguished this way and report
  not-streaming; callers fall back to the open state.
- **Microphones:** audio capture devices in active use. On desktops the signal
  is PipeWire (`pw-dump`), which covers USB, built-in, and Bluetooth (`bluez5`)
  mics and attributes capture to the client process. On headless/embedded hosts
  the fallback is raw ALSA (`/proc/asound/card*/pcm*c/sub*/status`, `RUNNING` +
  `owner_pid`).
- **Grouping:** modern UVC cameras expose several `/dev/videoN` nodes per
  physical camera (capture + metadata). gocamdet reports one entry per physical
  USB device, listing all of its nodes.

## What it delivers

1. **A Go module** exposing `Detect()` for cameras and `DetectWithMic()` for
   cameras plus microphones — returning the set of devices, whether each is
   open/streaming/in use, and which processes are using them.
2. **An example CLI** (`cmd/gocamdet`) that prints a human-readable snapshot by
   default (cameras and mics), supports `--json` for machine consumption, and
   sets a scriptable exit code (`0` = nothing streaming/capturing, `1` = a
   camera is streaming or a mic is in use, `2` = error). A camera that is only
   open does not set exit code `1`.
3. **A continuous watch mode** (`--watch`) that polls on an interval and runs a
   hook script on state transitions across four states — `off`, `mic-only-on`,
   `cam-only-on`, `both-on` — plus an example systemd unit to run it at boot.

## Design principles

- **Camera path is pure Go, no cgo, no libusb, no external tools.** Read
  directly from the kernel's own source of truth (sysfs/procfs). Zero external
  runtime dependencies; trivial to cross-compile.
- **Microphone path uses the running sound server.** Desktop mic detection
  shells out to `pw-dump` (part of PipeWire) because applications stream through
  the sound server rather than opening `/dev/snd` directly, and Bluetooth mics
  exist only inside PipeWire. Headless hosts use the pure-`/proc/asound`
  fallback. This is a deliberate, scoped exception to the "no external tools"
  rule, confined to desktop audio.
- **Graceful degradation on permissions and failures.** Reading another user's
  `/proc/[pid]/fd` requires root (or `CAP_SYS_PTRACE`); without it gocamdet still
  reports what it can see and flags reduced visibility. Mic-detection failures
  degrade to an empty mic list rather than failing the whole scan.
- **Testable without hardware or root.** Core camera logic is parameterized by a
  filesystem root so it can run against synthetic sysfs/proc trees; the watch
  loop takes an injected detection function and a caller-driven tick channel.
- **Explicit errors, no forgiving code.** Camera-environment precondition
  violations (no V4L2, no procfs) are hard errors; transient conditions (a device
  unplugged mid-scan, a process exiting, an unreadable fd) are tolerated and
  surfaced as reduced visibility.

## Explicit non-goals

- No frame-level content inspection. Streaming detection is transport-level (the
  USB VideoStreaming alternate setting), not an analysis of captured frames.
- No control of the camera or mic (start/stop/kill the using process).
- No macOS/Windows implementation (yet).
- No event-based (inotify/fanotify/udev) detection; watch mode polls, keeping the
  camera path zero-dependency and pure Go.
- No per-device transition granularity in watch mode; transitions are across the
  four aggregate states.
- No use of external tools on the camera path (`lsof`, `fuser`, `v4l2-ctl`). The
  desktop microphone path is the only exception (`pw-dump`).

## Success criteria

- `Detect()` correctly enumerates USB cameras, groups multi-node devices, and
  reports open/streaming status and the owning process (when visible).
- `DetectWithMic()` additionally reports microphones in active use and attributes
  them to the capturing process on both PipeWire and ALSA hosts.
- The CLI's exit code reliably reflects streaming/in-use status for shell
  scripting.
- The full test suite passes against synthetic fixtures without root or a real
  camera.
