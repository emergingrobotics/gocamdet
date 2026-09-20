package camdet

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// scanOpen walks root/proc/<pid>/fd looking for open file descriptors whose
// target is one of the tracked /dev/videoN nodes. It returns a map from node
// path to the processes holding it open, and whether visibility was complete.
//
// fullVisibility is false when at least one process's fd directory could not be
// read due to permissions, which happens when the scan is not run as root and
// another user owns the process. Such processes are skipped rather than failing
// the scan.
func scanOpen(root string, nodeSet map[string]bool) (map[string][]Process, bool, error) {
	procDir := filepath.Join(root, "proc")
	entries, err := os.ReadDir(procDir)
	if err != nil {
		// Absence of procfs is an environment precondition violation.
		return nil, false, fmt.Errorf("reading %s: %w", procDir, err)
	}

	openByNode := make(map[string][]Process)
	fullVisibility := true

	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			// Non-pid entries such as "self" or "sys".
			continue
		}

		fdDir := filepath.Join(procDir, entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			if os.IsPermission(err) {
				fullVisibility = false
			}
			// Otherwise the process exited mid-scan; skip it.
			continue
		}

		// Collect the camera nodes this process holds open.
		var openNodes []string
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				// The fd closed mid-scan; skip it.
				continue
			}
			if nodeSet[target] {
				openNodes = append(openNodes, target)
			}
		}
		if len(openNodes) == 0 {
			continue
		}

		comm := readComm(procDir, entry.Name())
		streaming := mappedNodes(procDir, entry.Name(), nodeSet)
		for _, node := range openNodes {
			openByNode[node] = append(openByNode[node], Process{
				PID:       pid,
				Name:      comm,
				Node:      node,
				Streaming: streaming[node],
			})
		}
	}

	return openByNode, fullVisibility, nil
}

// mappedNodes reads /proc/<pid>/maps and returns the set of tracked camera
// nodes the process has memory-mapped. A V4L2 capture mmaps its buffers from
// the device fd (after VIDIOC_REQBUFS), so an actively streaming process has the
// node mapped here; merely opening the device or probing its capabilities does
// not map anything. Unreadable maps (permissions, process exit) yield an empty
// set, i.e. "not streaming", degrading gracefully like the fd scan.
func mappedNodes(procDir, pid string, nodeSet map[string]bool) map[string]bool {
	mapped := make(map[string]bool)
	f, err := os.Open(filepath.Join(procDir, pid, "maps"))
	if err != nil {
		return mapped
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		// Each line is: address perms offset dev inode pathname
		fields := strings.Fields(sc.Text())
		if len(fields) < 6 {
			continue
		}
		if path := fields[len(fields)-1]; nodeSet[path] {
			mapped[path] = true
		}
	}
	return mapped
}

// readComm reads /proc/<pid>/comm, returning the process name without its
// trailing newline. A missing file yields an empty string.
func readComm(procDir, pid string) string {
	data, err := os.ReadFile(filepath.Join(procDir, pid, "comm"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
