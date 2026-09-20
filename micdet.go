// Package camdet detects whether USB cameras are currently in use on a Linux
// system by reading the kernel's V4L2, sysfs, and procfs interfaces directly.
//
// A camera is considered "in use" when any process holds one of its
// /dev/videoN nodes open. Only cameras whose sysfs path traces to a USB bus are
// reported. Reading another user's /proc/<pid>/fd requires root; without it the
// scan still returns what it can see and reports reduced visibility via
// Result.FullVisibility.
//
// Microphone detection is optional and depends on the sound server (PipeWire
// for desktops, raw ALSA for headless). See DetectMicInUse().
package camdet

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Mic represents an audio capture device and its current use state.
type Mic struct {
	Name    string `json:"name"`    // device name from PipeWire/ALSA
	Device  string `json:"device"`  // device identifier (PipeWire node name or ALSA subdevice path)
	InUse   bool   `json:"inUse"`   // true if any stream is capturing
	Users   []User `json:"users"`   // processes capturing audio; may be partial
	Backend string `json:"backend"` // "pipewire" or "alsa"
}

// User identifies a process capturing audio.
type User struct {
	PID  int    `json:"pid"`
	Name string `json:"name"` // from /proc/<pid>/comm
}

// hasPipeWire returns true if PipeWire or PulseAudio appears to be running.
func hasPipeWire() bool {
	// Check for pipewire or pulseaudio processes
	commands := []string{"pipewire", "wireplumber", "pulseaudio"}
	for _, cmd := range commands {
		if _, err := exec.LookPath(cmd); err == nil {
			if err := exec.Command("pgrep", "-x", cmd).Run(); err == nil {
				return true
			}
		}
	}
	return false
}

// detectMicPipeWire queries PipeWire for active audio capture streams.
// It returns mic state and users, or an error if pw-dump fails.
func detectMicPipeWire() ([]Mic, error) {
    // Query PipeWire for running audio capture nodes.
    // Return a slice of Mic, each representing a distinct device (source) that
    // is currently in use. For Stream/Input/Audio nodes we also attach the
    // capturing process as a user. Audio/Source nodes represent the underlying
    // hardware microphone; they may have no associated user if only the device
    // node is present.
    cmd := exec.Command("pw-dump")
    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("pw-dump failed: %w", err)
    }

    type Props struct {
        MediaClass       string `json:"media.class"`
        NodeName         string `json:"node.name"`
        AppProcessID     int    `json:"application.process.id"`
        AppProcessBinary string `json:"application.process.binary"`
        DeviceAPI        string `json:"device.api"`
    }
    type Info struct {
        State string `json:"state"`
        Props Props  `json:"props"`
    }
    type Node struct {
        Type string `json:"type"`
        Info Info   `json:"info"`
    }

    var nodes []Node
    if err := json.Unmarshal(output, &nodes); err != nil {
        return nil, fmt.Errorf("parsing pw-dump JSON: %w", err)
    }

    // Map from node name (device identifier) to Mic index.
    micMap := make(map[string]int)
    var mics []Mic

    for _, n := range nodes {
        if n.Type != "PipeWire:Interface:Node" {
            continue
        }
        if n.Info.State != "running" {
            continue
        }
        mc := n.Info.Props.MediaClass
        // Handle application capture streams.
        if mc == "Stream/Input/Audio" {
            // Create or update mic entry for the device name.
            name := n.Info.Props.NodeName
            idx, ok := micMap[name]
            if !ok {
                mics = append(mics, Mic{
                    Name:    name,
                    Device:  name,
                    InUse:   true,
                    Backend: "pipewire",
                })
                idx = len(mics) - 1
                micMap[name] = idx
            }
            // Attach user.
            if n.Info.Props.AppProcessID > 0 {
                mics[idx].Users = append(mics[idx].Users, User{PID: n.Info.Props.AppProcessID, Name: n.Info.Props.AppProcessBinary})
            }
            // Ensure InUse flag.
            mics[idx].InUse = true
        } else if mc == "Audio/Source" {
            // Hardware source device, may be Bluetooth (bluez5) or regular.
            name := n.Info.Props.NodeName
            if _, exists := micMap[name]; !exists {
                mics = append(mics, Mic{
                    Name:    name,
                    Device:  name,
                    InUse:   true,
                    Backend: "pipewire",
                })
                micMap[name] = len(mics) - 1
            }
        }
    }

    return mics, nil
}

// detectMicALSA scans ALSA capture devices via /proc/asound.
// This is the fallback for headless/embedded systems without PipeWire.
func detectMicALSA() ([]Mic, error) {
	// Scan /proc/asound/card*/pcm*c/sub*/status
	procRoot := "/proc/asound"
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		// No ALSA cards present
		return []Mic{}, nil
	}

	var mics []Mic

	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "card") {
			continue
		}
		cardPath := filepath.Join(procRoot, entry.Name())
		subEntries, err := os.ReadDir(cardPath)
		if err != nil {
			continue
		}

		// Capture PCMs are directories named like "pcm0c" (playback ends in "p").
		// The status file lives one level deeper, per subdevice:
		// /proc/asound/cardN/pcmNc/subN/status.
		for _, pcm := range subEntries {
			if !pcm.IsDir() || !strings.HasPrefix(pcm.Name(), "pcm") || !strings.HasSuffix(pcm.Name(), "c") {
				continue
			}
			pcmPath := filepath.Join(cardPath, pcm.Name())
			subs, err := os.ReadDir(pcmPath)
			if err != nil {
				continue
			}

			for _, sub := range subs {
				if !strings.HasPrefix(sub.Name(), "sub") {
					continue
				}
				statusPath := filepath.Join(pcmPath, sub.Name(), "status")
				data, err := os.ReadFile(statusPath)
				if err != nil {
					continue
				}

				status := strings.TrimSpace(string(data))
				if !strings.Contains(status, "state: RUNNING") {
					continue
				}

				// The kernel formats this line as "owner_pid   : 1234";
				// Sscanf spaces match any run of whitespace, colon is literal.
				var pid int
				for _, line := range strings.Split(status, "\n") {
					if strings.HasPrefix(strings.TrimSpace(line), "owner_pid") {
						fmt.Sscanf(strings.TrimSpace(line), "owner_pid : %d", &pid)
						break
					}
				}

				name := ""
				if pid > 0 {
					commPath := filepath.Join("/proc", fmt.Sprintf("%d", pid), "comm")
					if commData, err := os.ReadFile(commPath); err == nil {
						name = strings.TrimSpace(string(commData))
					}
				}

				mics = append(mics, Mic{
					Name:    fmt.Sprintf("%s - %s/%s", entry.Name(), pcm.Name(), sub.Name()),
					Device:  statusPath,
					InUse:   true,
					Users:   []User{{PID: pid, Name: name}},
					Backend: "alsa",
				})
			}
		}
	}

	return mics, nil
}

// DetectMicInUse scans for active audio capture (microphone use).
// It prefers PipeWire on desktop systems, falling back to raw ALSA on headless systems.
// Returns mic devices and whether any are in use.
func DetectMicInUse() ([]Mic, error) {
	// Try PipeWire first (desktop)
	if hasPipeWire() {
		mics, err := detectMicPipeWire()
		if err != nil {
			// Fall back to ALSA if PipeWire fails
			return detectMicALSA()
		}
		return mics, nil
	}

	// Fall back to ALSA (headless/embedded)
	return detectMicALSA()
}

// MicResult wraps camera and microphone detection results.
type MicResult struct {
	*Result
	Mics []Mic `json:"mics"`
}

// DetectWithMic returns both camera and microphone detection results.
func DetectWithMic() (*MicResult, error) {
	camRes, err := Detect()
	if err != nil {
		return nil, err
	}

	mics, err := DetectMicInUse()
	if err != nil {
		// Don't fail the whole scan if mic detection fails
		mics = []Mic{}
	}

	return &MicResult{
		Result: camRes,
		Mics:   mics,
	}, nil
}
