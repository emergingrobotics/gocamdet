// Package camdet detects whether USB cameras are currently in use on a Linux
// system by reading the kernel's V4L2, sysfs, and procfs interfaces directly.
//
// A camera is considered "in use" when any process holds one of its
// /dev/videoN nodes open. Only cameras whose sysfs path traces to a USB bus are
// reported. Reading another user's /proc/<pid>/fd requires root; without it the
// scan still returns what it can see and reports reduced visibility via
// Result.FullVisibility.
package camdet

import (
	"sort"
)

// Camera is one physical USB camera, aggregating all of its V4L2 device nodes.
type Camera struct {
	Name      string    `json:"name"`      // model name from sysfs (e.g. "HD Pro Webcam C920")
	USBPath   string    `json:"usbPath"`   // physical-device key, e.g. "/sys/bus/usb/devices/1-2"
	VendorID  string    `json:"vendorId"`  // 4-hex USB vendor id, e.g. "046d"
	ProductID string    `json:"productId"` // 4-hex USB product id, e.g. "082d"
	Serial    string    `json:"serial"`    // may be empty if not exposed
	Nodes     []string  `json:"nodes"`     // e.g. ["/dev/video0", "/dev/video1"]
	InUse     bool      `json:"inUse"`     // true if any node is held open by a process
	Users     []Process `json:"users"`     // processes holding a node open; may be partial
}

// Process identifies a process holding a camera node open.
type Process struct {
	PID  int    `json:"pid"`
	Name string `json:"name"` // from /proc/<pid>/comm
	Node string `json:"node"` // which /dev/videoN this process has open
}

// Result is a full detection snapshot.
type Result struct {
	Cameras []Camera `json:"cameras"`
	// FullVisibility is false if some /proc entries were unreadable due to
	// permissions (i.e. the process is not running as root), meaning per-camera
	// Users lists may be incomplete.
	FullVisibility bool `json:"fullVisibility"`
}

// Detect scans the live system and returns the current snapshot of USB cameras.
func Detect() (*Result, error) {
	return detect("/")
}

// detect performs the scan against the given filesystem root. Production uses
// root "/"; tests point it at a synthetic sysfs/proc tree.
func detect(root string) (*Result, error) {
	nodes, err := enumerate(root)
	if err != nil {
		return nil, err
	}

	// Group nodes by physical USB device.
	byUSB := make(map[string]*Camera)
	var order []string
	nodeSet := make(map[string]bool)
	for _, n := range nodes {
		nodeSet[n.devNode] = true
		cam, ok := byUSB[n.usbPath]
		if !ok {
			cam = &Camera{
				Name:      n.name,
				USBPath:   n.usbPath,
				VendorID:  n.vendorID,
				ProductID: n.productID,
				Serial:    n.serial,
			}
			byUSB[n.usbPath] = cam
			order = append(order, n.usbPath)
		}
		cam.Nodes = append(cam.Nodes, n.devNode)
	}

	openByNode, fullVisibility, err := scanOpen(root, nodeSet)
	if err != nil {
		return nil, err
	}

	// Attribute open handles to their owning camera.
	for _, cam := range byUSB {
		sort.Strings(cam.Nodes)
		for _, node := range cam.Nodes {
			for _, proc := range openByNode[node] {
				cam.InUse = true
				cam.Users = append(cam.Users, proc)
			}
		}
		sort.Slice(cam.Users, func(i, j int) bool {
			if cam.Users[i].PID != cam.Users[j].PID {
				return cam.Users[i].PID < cam.Users[j].PID
			}
			return cam.Users[i].Node < cam.Users[j].Node
		})
	}

	// Emit cameras in a deterministic order (by USB path).
	sort.Strings(order)
	cameras := make([]Camera, 0, len(order))
	for _, usbPath := range order {
		cameras = append(cameras, *byUSB[usbPath])
	}

	return &Result{Cameras: cameras, FullVisibility: fullVisibility}, nil
}
