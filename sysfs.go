package camdet

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// nodeInfo is a single /dev/videoN node that has been confirmed to belong to a
// USB camera, together with the identity of that physical USB device.
type nodeInfo struct {
	devNode   string // e.g. "/dev/video0"
	usbPath   string // resolved USB device directory, the physical-device key
	name      string
	vendorID  string
	productID string
	serial    string
}

// enumerate lists all USB camera nodes under the given filesystem root.
//
// It reads root/sys/class/video4linux, resolves each videoN class symlink to
// its device directory, and keeps only those whose ancestry includes a USB
// device descriptor (a directory containing idVendor and idProduct). That
// directory is the physical-device key used for grouping.
func enumerate(root string) ([]nodeInfo, error) {
	base := filepath.Join(root, "sys", "class", "video4linux")
	entries, err := os.ReadDir(base)
	if err != nil {
		// Absence of the V4L2 class is an environment precondition violation.
		return nil, fmt.Errorf("reading %s: %w", base, err)
	}

	var nodes []nodeInfo
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "video") {
			continue
		}

		classLink := filepath.Join(base, name)
		deviceDir, err := resolveLink(classLink)
		if err != nil {
			// The node vanished mid-scan (device unplugged); tolerate it.
			continue
		}

		usbDir, found := findUSBDeviceDir(deviceDir)
		if !found {
			// Not a USB device (built-in/MIPI, PCI, virtual); excluded by scope.
			continue
		}

		info := nodeInfo{
			devNode:   "/dev/" + name,
			usbPath:   usbDir,
			vendorID:  readAttr(usbDir, "idVendor"),
			productID: readAttr(usbDir, "idProduct"),
			serial:    readAttr(usbDir, "serial"),
		}
		info.name = deviceName(usbDir, deviceDir)
		nodes = append(nodes, info)
	}

	return nodes, nil
}

// findUSBDeviceDir walks up from a V4L2 device directory looking for the nearest
// ancestor that is a USB device descriptor directory (one containing both
// idVendor and idProduct). It returns that directory and true, or false if the
// device does not sit on a USB bus.
func findUSBDeviceDir(start string) (string, bool) {
	dir := start
	for {
		if fileExists(filepath.Join(dir, "idVendor")) && fileExists(filepath.Join(dir, "idProduct")) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// deviceName prefers the USB product string, falling back to the V4L2 "name"
// attribute exposed on the video4linux device directory.
func deviceName(usbDir, deviceDir string) string {
	if product := readAttr(usbDir, "product"); product != "" {
		return product
	}
	return readAttr(deviceDir, "name")
}

// resolveLink reads a symlink and returns its target resolved against the
// link's own directory (sysfs uses relative symlinks). Non-symlink paths are
// returned cleaned as-is.
func resolveLink(linkPath string) (string, error) {
	target, err := os.Readlink(linkPath)
	if err != nil {
		// Not a symlink, or it disappeared. If it is a real directory, use it.
		if info, statErr := os.Stat(linkPath); statErr == nil && info.IsDir() {
			return filepath.Clean(linkPath), nil
		}
		return "", err
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target), nil
	}
	return filepath.Clean(filepath.Join(filepath.Dir(linkPath), target)), nil
}

// readAttr reads a single-line sysfs attribute file, trimming whitespace.
// A missing or unreadable attribute yields an empty string.
func readAttr(dir, name string) string {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
