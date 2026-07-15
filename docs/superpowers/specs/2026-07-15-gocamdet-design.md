# gocamdet — Design Spec

Date: 2026-07-15
Status: Approved for planning

## Summary

A pure-Go Linux module and example CLI that detect whether a USB camera is
currently in use (held open by any process) and report which process is using
it. See `VISION.md` for scope and non-goals.

## Decisions (from brainstorming)

| Decision | Choice |
| --- | --- |
| Platforms | Linux only; API kept backend-agnostic for future ports |
| "In use" definition | Any process holds a `/dev/videoN` node open (via `/proc/*/fd`) |
| Device scope | USB cameras only (sysfs path must trace to a USB bus) |
| Grouping | One entry per physical USB device; in-use if any node is open |
| CLI | One-shot; human table default, `--json`, scriptable exit code |
| Implementation | Pure Go, no cgo/libusb, no external tools at runtime |
| Permissions | Degrade gracefully; flag reduced visibility instead of failing |

## Module layout

```
gocamdet/
  go.mod                      module github.com/gherlein/gocamdet
  camdet.go                   public API (types + Detect())
  sysfs.go                    enumerate + USB filter + grouping
  procscan.go                 /proc/*/fd open-handle scanning
  camdet_test.go              tests against synthetic sysfs/proc trees
  cmd/gocamdet/main.go        example CLI
  README.md
  Makefile                    build/test/lint targets
```

## Public API

```go
package camdet

// Camera is one physical USB camera, aggregating all of its V4L2 nodes.
type Camera struct {
    Name      string    // model name from sysfs product (e.g. "HD Pro Webcam C920")
    USBPath   string    // physical-device key, e.g. "/sys/bus/usb/devices/1-2"
    VendorID  string    // 4-hex, e.g. "046d"
    ProductID string    // 4-hex, e.g. "082d"
    Serial    string    // may be empty if not exposed
    Nodes     []string  // e.g. ["/dev/video0", "/dev/video1"]
    InUse     bool      // any node held open by a process
    Users     []Process // processes holding a node open; may be partial (see Result)
}

// Process identifies a process holding a camera node open.
type Process struct {
    PID  int
    Name string // /proc/<pid>/comm
    Node string // which /dev/videoN this process has open
}

// Result is the full detection snapshot.
type Result struct {
    Cameras        []Camera
    FullVisibility bool // false if some /proc entries were unreadable (not root)
}

// Detect scans the live system (root "/") and returns the current snapshot.
func Detect() (*Result, error)
```

Internally, the work is done by an unexported `detect(root string) (*Result, error)`
so tests can point at a synthetic tree. `Detect()` is the `root = "/"` wrapper.

## Data flow

1. **Enumerate** — read `root/sys/class/video4linux/`; each `videoN` entry is a
   candidate node.
2. **USB filter + identity** — resolve `.../videoN/device` and walk the symlink
   chain toward the bus root. Keep the node only if an ancestor lies under
   `sys/bus/usb`. The nearest enclosing USB *device* directory (the one holding
   `idVendor`/`idProduct`) is the physical-device key. Read `idVendor`,
   `idProduct`, `serial`, and `product` from it.
3. **Group** — bucket nodes by physical-device key; produce one `Camera` per
   bucket with `Nodes` sorted.
4. **Open-scan** — walk `root/proc/*/fd/*`. For each symlink whose target is one
   of the tracked `/dev/videoN` nodes, record `{PID, comm, node}` and attribute
   it to the owning `Camera`, setting `InUse = true`. Deduplicate `Users` by
   `(PID, Node)`.
5. **Visibility** — if any `proc/<pid>/fd` directory is unreadable due to
   permissions (EACCES), set `FullVisibility = false`.

## Error handling

Per project engineering rules (preconditions, explicit errors, no forgiving
code, no defensive swallowing):

- **Hard errors (returned):**
  - `root/sys/class/video4linux` missing or unreadable → not a V4L2 system.
  - `root/proc` missing or unreadable → cannot determine open handles.
- **Tolerated conditions (not errors; affect data, not control flow):**
  - A `videoN` or its sysfs attribute vanishing mid-scan (device unplugged) →
    that node/camera is dropped from the snapshot.
  - A `/proc/<pid>` disappearing mid-scan (process exited) → skip that pid.
  - EACCES reading another user's `fd` dir → skip it and set
    `FullVisibility = false`.
- Errors are wrapped with context (`fmt.Errorf("...: %w", err)`). No error is
  silently discarded; tolerated conditions are explicit, commented branches.

## CLI behavior (`cmd/gocamdet`)

- **Default output:** human-readable table — camera name, `vendor:product`,
  nodes, in-use (yes/no), and using PIDs/names.
- **`--json`:** marshal `Result` to stdout (pretty-printed).
- **Exit codes:** `0` = no camera in use, `1` = at least one in use, `2` = error.
- **Reduced-visibility note:** when `FullVisibility` is false, print a one-line
  note to **stderr** (suggesting `sudo` for complete results) so it never
  corrupts `--json` on stdout.

## Testing

- Core logic is exercised via `detect(root)` against synthetic trees built in
  `t.TempDir()`, using real files and symlinks — no root, no hardware.
- Fixture helper builds: a `sys/class/video4linux/videoN` node linked to a
  fabricated USB device dir (with `idVendor`/`idProduct`/`product`/`serial`),
  plus `proc/<pid>/fd/<n>` symlinks pointing at `/dev/videoN` and
  `proc/<pid>/comm`.
- Required cases:
  1. No cameras → empty `Cameras`, `InUse` all false.
  2. One USB camera, idle → present, `InUse == false`.
  3. One USB camera, in use → `InUse == true`, `Users` has the fake PID/comm.
  4. Multi-node camera (video0 + video1, same USB device) → grouped into one
     `Camera` with two `Nodes`.
  5. Non-USB device (e.g. PCI/platform path) → excluded from results.
  6. Unreadable `proc/<pid>/fd` (EACCES) → `FullVisibility == false`, no crash.
  7. Two cameras, one in use → correct per-camera `InUse`; CLI exit code `1`.
- All tests run via `make test`; no stubbed, skipped, or commented-out tests.

## Build

- `Makefile` with at least: `build`, `test`, `vet`, `lint`, `clean`.
- No network or external tool dependencies at build or run time.

## Open items for the plan

- Exact sysfs walk termination rule (how far up to stop when identifying the USB
  device dir) — pin down in the implementation plan with a concrete algorithm.
- Table formatting library vs `text/tabwriter` (lean toward stdlib
  `text/tabwriter` to keep zero-deps).
