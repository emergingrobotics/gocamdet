package camdet

import (
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
		for _, node := range openNodes {
			openByNode[node] = append(openByNode[node], Process{
				PID:  pid,
				Name: comm,
				Node: node,
			})
		}
	}

	return openByNode, fullVisibility, nil
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
