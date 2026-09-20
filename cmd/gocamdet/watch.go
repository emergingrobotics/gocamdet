package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gherlein/gocamdet"
)

// Hook events. The hook script receives one of these as its first argument and
// as CAMDET_EVENT.
const (
	eventMicOnlyOn = "mic-only-on"
	eventCamOnlyOn = "cam-only-on"
	eventBothOn    = "both-on"
	eventOff       = "off"
)

// eventInfo tracks camera and mic in-use state.
type eventInfo struct {
	camerasInUse int
	micsInUse    int
}

// anyInUse reports whether any camera is actively streaming or any microphone
// is in use. A camera that is merely open (e.g. an app probing device
// capabilities) is not "in use" for transition purposes — only active capture
// (Streaming) counts, so probes do not fire the hook.
func anyInUse(res *camdet.MicResult) bool {
	for _, cam := range res.Cameras {
		if cam.Streaming {
			return true
		}
	}
	for _, mic := range res.Mics {
		if mic.InUse {
			return true
		}
	}
	return false
}

// getEventInfo returns the count of actively-streaming cameras and in-use mics.
func getEventInfo(res *camdet.MicResult) eventInfo {
	var info eventInfo
	for _, cam := range res.Cameras {
		if cam.Streaming {
			info.camerasInUse++
		}
	}
	for _, mic := range res.Mics {
		if mic.InUse {
			info.micsInUse++
		}
	}
	return info
}

// eventFor maps the current and previous in-use state to its hook event name.
// prevInfo and currInfo are the previous and current camera/mic state.
func eventFor(prevInfo, currInfo eventInfo) string {
	// If we're going from all off to some on, determine which type
	if prevInfo.camerasInUse == 0 && prevInfo.micsInUse == 0 {
		if currInfo.camerasInUse > 0 && currInfo.micsInUse > 0 {
			return eventBothOn
		}
		if currInfo.camerasInUse > 0 {
			return eventCamOnlyOn
		}
		if currInfo.micsInUse > 0 {
			return eventMicOnlyOn
		}
	}

	// If we're going from some on to all off
	if prevInfo.camerasInUse > 0 || prevInfo.micsInUse > 0 {
		if currInfo.camerasInUse == 0 && currInfo.micsInUse == 0 {
			return eventOff
		}
	}

	// No transition (same state) or partial transition within "on" state
	// Don't fire for transitions within "on" state (e.g., cam-only to both)
	return ""
}

// hookEnv builds the environment variables handed to the hook script. The
// per-camera lists describe only the cameras currently in use, so they are
// empty for an "off" event.
func hookEnv(event string, res *camdet.MicResult, now time.Time) []string {
	var camNames, camIds, camUsers []string
	var micNames, micUsers []string
	camCount := 0
	micCount := 0
	for _, cam := range res.Cameras {
		if !cam.Streaming {
			continue
		}
		camCount++
		camNames = append(camNames, cam.Name)
		camIds = append(camIds, cam.VendorID+":"+cam.ProductID)
		for _, u := range cam.Users {
			camUsers = append(camUsers, fmt.Sprintf("%s(%d)", u.Name, u.PID))
		}
	}
	for _, mic := range res.Mics {
		if !mic.InUse {
			continue
		}
		micCount++
		micNames = append(micNames, mic.Name)
		for _, u := range mic.Users {
			micUsers = append(micUsers, fmt.Sprintf("%s(%d)", u.Name, u.PID))
		}
	}
	return []string{
		"CAMDET_EVENT=" + event,
		"CAMDET_TIMESTAMP=" + now.Format(time.RFC3339),
		"CAMDET_CAMERAS_IN_USE=" + strconv.Itoa(camCount),
		"CAMDET_CAMERA_NAMES=" + strings.Join(camNames, ","),
		"CAMDET_CAMERA_IDS=" + strings.Join(camIds, ","),
		"CAMDET_USERS=" + strings.Join(camUsers, ","),
		"CAMDET_MICS_IN_USE=" + strconv.Itoa(micCount),
		"CAMDET_MIC_NAMES=" + strings.Join(micNames, ","),
		"CAMDET_MIC_USERS=" + strings.Join(micUsers, ","),
	}
}

// runScript executes the hook synchronously, passing the event as its argument
// and the snapshot details via the environment. It is bounded by timeout so a
// wedged hook cannot stall the watch loop.
func runScript(ctx context.Context, scriptPath string, timeout time.Duration, event string, res *camdet.MicResult, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, scriptPath, event)
	cmd.Env = append(os.Environ(), hookEnv(event, res, now)...)
	// Run the hook in its own process group and, on timeout, signal the whole
	// group. Otherwise a child spawned by the hook (e.g. sleep) keeps the output
	// pipe open and CombinedOutput would block until it exits despite the kill.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = timeout
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("hook %q %s: %w: %s", scriptPath, event, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// transitionRecord is the JSON shape emitted per transition in --json mode.
type transitionRecord struct {
	Event     string          `json:"event"`
	Timestamp string          `json:"timestamp"`
	Cameras   []camdet.Camera `json:"camerasInUse"`
	Mics      []camdet.Mic    `json:"micsInUse"`
}

// logTransition writes a one-line human record, or a single JSON object, for a
// state change to w.
func logTransition(w io.Writer, event string, res *camdet.MicResult, now time.Time, asJSON bool) {
	cams := inUseCameras(res)
	mics := inUseMics(res)

	if asJSON {
		enc := json.NewEncoder(w)
		_ = enc.Encode(transitionRecord{
			Event:     event,
			Timestamp: now.Format(time.RFC3339),
			Cameras:   cams,
			Mics:      mics,
		})
		return
	}

	stamp := now.Format(time.RFC3339)
	if event == eventOff {
		fmt.Fprintf(w, "%s device off\n", stamp)
		return
	}

	// Determine which type of event for human-readable output
	var deviceType string
	switch event {
	case eventMicOnlyOn:
		deviceType = "mic"
	case eventCamOnlyOn:
		deviceType = "camera"
	case eventBothOn:
		deviceType = "camera and mic"
	}

	parts := make([]string, 0, len(cams)+len(mics))
	for _, cam := range cams {
		parts = append(parts, fmt.Sprintf("%s (%s:%s) used by %s",
			cam.Name, cam.VendorID, cam.ProductID, formatUsers(cam.Users)))
	}
	for _, mic := range mics {
		parts = append(parts, fmt.Sprintf("%s (%s) used by %s",
			mic.Name, mic.Device, formatMicUsers(mic.Users)))
	}
	if len(cams) == 0 && len(mics) == 0 {
		fmt.Fprintf(w, "%s %s off\n", stamp, deviceType)
	} else {
		fmt.Fprintf(w, "%s %s on — %s\n", stamp, deviceType, strings.Join(parts, "; "))
	}
}

func inUseCameras(res *camdet.MicResult) []camdet.Camera {
	var cams []camdet.Camera
	for _, cam := range res.Cameras {
		if cam.Streaming {
			cams = append(cams, cam)
		}
	}
	return cams
}

func inUseMics(res *camdet.MicResult) []camdet.Mic {
	var mics []camdet.Mic
	for _, mic := range res.Mics {
		if mic.InUse {
			mics = append(mics, mic)
		}
	}
	return mics
}

// runWatch runs the continuous watch loop until SIGINT/SIGTERM, wiring live
// detection to console logging and the optional hook script. It returns the
// process exit code: 0 on clean shutdown, exitError on a fatal detection error.
func runWatch(scriptPath string, interval, scriptTimeout time.Duration, asJSON bool) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	notedVisibility := false
	w := &watcher{
		detect: camdet.DetectWithMic,
		onEvent: func(ctx context.Context, event string, res *camdet.MicResult) {
			if !notedVisibility {
				notedVisibility = true
				if !res.FullVisibility {
					fmt.Fprintln(os.Stderr, "gocamdet: reduced visibility — not all processes could be inspected; run as root for complete results")
				}
			}
			logTransition(os.Stdout, event, res, time.Now(), asJSON)
			if scriptPath == "" {
				return
			}
			if err := runScript(ctx, scriptPath, scriptTimeout, event, res, time.Now()); err != nil {
				fmt.Fprintf(os.Stderr, "gocamdet: %v\n", err)
			}
		},
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if err := w.run(ctx, ticker.C); err != nil {
		fmt.Fprintf(os.Stderr, "gocamdet: %v\n", err)
		return exitError
	}
	return exitNoneInUse
}

// watcher polls detect and reports on/off transitions through onEvent.
// It is decoupled from the clock and the hook so the loop can be driven and
// observed in tests.
type watcher struct {
	detect  func() (*camdet.MicResult, error)
	onEvent func(ctx context.Context, event string, res *camdet.MicResult)
}

// run establishes the current state and fires onEvent immediately
// (fire-on-startup), then re-detects on every receive from tick and fires
// onEvent only when the state changes. It returns when ctx is
// cancelled, or with an error if a detection fails (a detection error is an
// environment precondition violation, not a transient condition).
func (w *watcher) run(ctx context.Context, tick <-chan time.Time) error {
	res, err := w.detect()
	if err != nil {
		return err
	}
	prevInfo := getEventInfo(res)
	event := eventFor(eventInfo{}, prevInfo)
	if event != "" {
		w.onEvent(ctx, event, res)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick:
			res, err := w.detect()
			if err != nil {
				return err
			}
			currInfo := getEventInfo(res)
			if event := eventFor(prevInfo, currInfo); event != "" {
				prevInfo = currInfo
				w.onEvent(ctx, event, res)
			}
		}
	}
}

// formatUsers formats a list of processes for display.
func formatUsers(users []camdet.Process) string {
	if len(users) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(users))
	for _, u := range users {
		parts = append(parts, fmt.Sprintf("%s(%d)", u.Name, u.PID))
	}
	return strings.Join(parts, ",")
}
