package camdet

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// fixture builds a synthetic filesystem root with a sysfs and proc tree so the
// scan can be exercised without root privileges or real hardware.
type fixture struct {
	t    *testing.T
	root string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return &fixture{t: t, root: t.TempDir()}
}

// addUSBCamera creates a USB device directory with the given identity and one
// video4linux node per name in nodes (all under the same physical device, so
// they group together). It mirrors real sysfs relative symlinks.
func (f *fixture) addUSBCamera(deviceKey, vendor, product, name, serial string, nodes ...string) {
	f.t.Helper()
	usbDir := filepath.Join(f.root, "sys", "devices", "usbtree", deviceKey)
	ifaceDir := filepath.Join(usbDir, deviceKey+":1.0")
	mustMkdir(f.t, ifaceDir)
	mustWrite(f.t, filepath.Join(usbDir, "idVendor"), vendor+"\n")
	mustWrite(f.t, filepath.Join(usbDir, "idProduct"), product+"\n")
	if product != "" {
		mustWrite(f.t, filepath.Join(usbDir, "product"), name+"\n")
	}
	if serial != "" {
		mustWrite(f.t, filepath.Join(usbDir, "serial"), serial+"\n")
	}

	classDir := filepath.Join(f.root, "sys", "class", "video4linux")
	mustMkdir(f.t, classDir)
	for _, node := range nodes {
		nodeDir := filepath.Join(ifaceDir, "video4linux", node)
		mustMkdir(f.t, nodeDir)
		rel, err := filepath.Rel(classDir, nodeDir)
		if err != nil {
			f.t.Fatalf("computing relative link: %v", err)
		}
		if err := os.Symlink(rel, filepath.Join(classDir, node)); err != nil {
			f.t.Fatalf("symlinking class node: %v", err)
		}
	}
}

// addNonUSBCamera creates a video4linux node on a non-USB (platform) path.
func (f *fixture) addNonUSBCamera(node string) {
	f.t.Helper()
	nodeDir := filepath.Join(f.root, "sys", "devices", "platform", "soc", "camera", "video4linux", node)
	mustMkdir(f.t, nodeDir)
	classDir := filepath.Join(f.root, "sys", "class", "video4linux")
	mustMkdir(f.t, classDir)
	rel, err := filepath.Rel(classDir, nodeDir)
	if err != nil {
		f.t.Fatalf("computing relative link: %v", err)
	}
	if err := os.Symlink(rel, filepath.Join(classDir, node)); err != nil {
		f.t.Fatalf("symlinking class node: %v", err)
	}
}

// addProcessUsing creates a /proc/<pid> entry whose fd points at devNode.
func (f *fixture) addProcessUsing(pid int, comm, devNode string) {
	f.t.Helper()
	fdDir := filepath.Join(f.root, "proc", strconv.Itoa(pid), "fd")
	mustMkdir(f.t, fdDir)
	mustWrite(f.t, filepath.Join(f.root, "proc", strconv.Itoa(pid), "comm"), comm+"\n")
	if err := os.Symlink(devNode, filepath.Join(fdDir, "3")); err != nil {
		f.t.Fatalf("symlinking fd: %v", err)
	}
}

// addStreamingProcess creates a /proc/<pid> entry that both holds devNode open
// and has it memory-mapped, simulating an active V4L2 capture (which mmaps its
// buffers from the device fd).
func (f *fixture) addStreamingProcess(pid int, comm, devNode string) {
	f.t.Helper()
	f.addProcessUsing(pid, comm, devNode)
	line := fmt.Sprintf("7f0000000000-7f0000001000 rw-s 00000000 00:06 1145 %s\n", devNode)
	mustWrite(f.t, filepath.Join(f.root, "proc", strconv.Itoa(pid), "maps"), line)
}

// addUnreadableProcess creates a /proc/<pid>/fd directory that cannot be read,
// simulating another user's process when not running as root.
func (f *fixture) addUnreadableProcess(pid int) {
	f.t.Helper()
	fdDir := filepath.Join(f.root, "proc", strconv.Itoa(pid), "fd")
	mustMkdir(f.t, fdDir)
	if err := os.Chmod(fdDir, 0o000); err != nil {
		f.t.Fatalf("chmod fd dir: %v", err)
	}
	f.t.Cleanup(func() { _ = os.Chmod(fdDir, 0o755) })
}

// ensureProc guarantees a /proc directory exists even with no processes, since
// scanOpen treats a missing procfs as a hard error.
func (f *fixture) ensureProc() {
	f.t.Helper()
	mustMkdir(f.t, filepath.Join(f.root, "proc"))
}

func TestNoCameras(t *testing.T) {
	f := newFixture(t)
	mustMkdir(t, filepath.Join(f.root, "sys", "class", "video4linux"))
	f.ensureProc()

	res, err := detect(f.root)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(res.Cameras) != 0 {
		t.Fatalf("expected 0 cameras, got %d", len(res.Cameras))
	}
	if !res.FullVisibility {
		t.Fatalf("expected full visibility")
	}
}

func TestOneCameraIdle(t *testing.T) {
	f := newFixture(t)
	f.addUSBCamera("1-2", "046d", "082d", "HD Pro Webcam C920", "ABC123", "video0")
	f.ensureProc()

	res, err := detect(f.root)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(res.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(res.Cameras))
	}
	cam := res.Cameras[0]
	if cam.InUse {
		t.Fatalf("expected idle camera")
	}
	if cam.Name != "HD Pro Webcam C920" || cam.VendorID != "046d" || cam.ProductID != "082d" || cam.Serial != "ABC123" {
		t.Fatalf("unexpected camera identity: %+v", cam)
	}
	if len(cam.Nodes) != 1 || cam.Nodes[0] != "/dev/video0" {
		t.Fatalf("unexpected nodes: %v", cam.Nodes)
	}
}

func TestOneCameraInUse(t *testing.T) {
	f := newFixture(t)
	f.addUSBCamera("1-2", "046d", "082d", "HD Pro Webcam C920", "ABC123", "video0")
	f.addProcessUsing(1234, "zoom", "/dev/video0")

	res, err := detect(f.root)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	cam := res.Cameras[0]
	if !cam.InUse {
		t.Fatalf("expected in-use camera")
	}
	if len(cam.Users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(cam.Users))
	}
	u := cam.Users[0]
	if u.PID != 1234 || u.Name != "zoom" || u.Node != "/dev/video0" {
		t.Fatalf("unexpected user: %+v", u)
	}
}

func TestMultiNodeGrouping(t *testing.T) {
	f := newFixture(t)
	f.addUSBCamera("1-2", "046d", "082d", "HD Pro Webcam C920", "ABC123", "video0", "video1")
	f.ensureProc()

	res, err := detect(f.root)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(res.Cameras) != 1 {
		t.Fatalf("expected multi-node camera grouped into 1, got %d", len(res.Cameras))
	}
	nodes := res.Cameras[0].Nodes
	if len(nodes) != 2 || nodes[0] != "/dev/video0" || nodes[1] != "/dev/video1" {
		t.Fatalf("unexpected grouped nodes: %v", nodes)
	}
}

func TestNonUSBExcluded(t *testing.T) {
	f := newFixture(t)
	f.addUSBCamera("1-2", "046d", "082d", "HD Pro Webcam C920", "ABC123", "video0")
	f.addNonUSBCamera("video9")
	f.ensureProc()

	res, err := detect(f.root)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(res.Cameras) != 1 {
		t.Fatalf("expected non-USB device excluded, got %d cameras", len(res.Cameras))
	}
	if res.Cameras[0].USBPath == "" {
		t.Fatalf("expected the USB camera to survive")
	}
}

func TestReducedVisibility(t *testing.T) {
	f := newFixture(t)
	f.addUSBCamera("1-2", "046d", "082d", "HD Pro Webcam C920", "ABC123", "video0")
	f.addUnreadableProcess(9999)

	res, err := detect(f.root)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if res.FullVisibility {
		t.Fatalf("expected reduced visibility when a proc fd dir is unreadable")
	}
}

func TestTwoCamerasOneInUse(t *testing.T) {
	f := newFixture(t)
	f.addUSBCamera("1-2", "046d", "082d", "HD Pro Webcam C920", "ABC123", "video0")
	f.addUSBCamera("3-1", "1234", "5678", "Some Other Cam", "", "video2")
	f.addProcessUsing(1111, "obs", "/dev/video2")

	res, err := detect(f.root)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(res.Cameras) != 2 {
		t.Fatalf("expected 2 cameras, got %d", len(res.Cameras))
	}
	// Cameras are sorted by USB path; "1-2" sorts before "3-1".
	if res.Cameras[0].InUse {
		t.Fatalf("expected first camera idle")
	}
	if !res.Cameras[1].InUse {
		t.Fatalf("expected second camera in use")
	}
	if anyInUse := res.Cameras[0].InUse || res.Cameras[1].InUse; !anyInUse {
		t.Fatalf("expected at least one in use")
	}
}

func TestNotV4L2System(t *testing.T) {
	f := newFixture(t)
	f.ensureProc()
	// No sys/class/video4linux directory at all.
	if _, err := detect(f.root); err == nil {
		t.Fatalf("expected hard error when V4L2 class is absent")
	}
}

// --- small helpers ---

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// A camera is Streaming only when a process has a node memory-mapped (active
// capture). A process that merely holds the fd open (a probe) is InUse but not
// Streaming — this is the distinction that keeps browser device-enumeration from
// counting as "in use".
func TestStreamingVsOpen(t *testing.T) {
	f := newFixture(t)
	f.addUSBCamera("1-2", "046d", "082d", "Brio", "", "video0")
	f.addStreamingProcess(4321, "ffmpeg", "/dev/video0") // fd + mmap = streaming
	f.addProcessUsing(9876, "chrome", "/dev/video0")     // fd only = probe

	res, err := detect(f.root)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(res.Cameras) != 1 {
		t.Fatalf("want 1 camera, got %d", len(res.Cameras))
	}
	cam := res.Cameras[0]
	if !cam.InUse {
		t.Fatal("camera should be InUse (fds are open)")
	}
	if !cam.Streaming {
		t.Fatal("camera should be Streaming (ffmpeg has it mmap'd)")
	}
	streamingByName := map[string]bool{}
	for _, u := range cam.Users {
		streamingByName[u.Name] = u.Streaming
	}
	if !streamingByName["ffmpeg"] {
		t.Fatal("ffmpeg should be marked streaming")
	}
	if streamingByName["chrome"] {
		t.Fatal("chrome (probe, no mmap) must not be marked streaming")
	}
}

func TestOpenOnlyIsNotStreaming(t *testing.T) {
	f := newFixture(t)
	f.addUSBCamera("1-2", "046d", "082d", "Brio", "", "video0")
	f.addProcessUsing(5555, "chrome", "/dev/video0") // only opens, never mmaps

	res, err := detect(f.root)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	cam := res.Cameras[0]
	if !cam.InUse {
		t.Fatal("open fd should count as InUse")
	}
	if cam.Streaming {
		t.Fatal("an open-only camera must not be Streaming")
	}
}
