package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gherlein/gocamdet"
)

func idleResult() *camdet.MicResult {
	return &camdet.MicResult{
		Result: &camdet.Result{
			Cameras: []camdet.Camera{{
				Name:      "HD Pro Webcam C920",
				VendorID:  "046d",
				ProductID: "082d",
				Nodes:     []string{"/dev/video0"},
				InUse:     false,
			}},
			FullVisibility: true,
		},
	}
}

func inUseResult() *camdet.MicResult {
	return &camdet.MicResult{
		Result: &camdet.Result{
			Cameras: []camdet.Camera{{
				Name:      "HD Pro Webcam C920",
				VendorID:  "046d",
				ProductID: "082d",
				Nodes:     []string{"/dev/video0"},
				InUse:     true,
				Streaming: true,
				Users:     []camdet.Process{{PID: 1234, Name: "zoom", Node: "/dev/video0", Streaming: true}},
			}},
			FullVisibility: true,
		},
	}
}

func envMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, e := range env {
		if i := strings.IndexByte(e, '='); i >= 0 {
			m[e[:i]] = e[i+1:]
		}
	}
	return m
}

func TestAnyInUse(t *testing.T) {
	if anyInUse(idleResult()) {
		t.Fatal("idle result should not be in use")
	}
	if !anyInUse(inUseResult()) {
		t.Fatal("in-use result should be in use")
	}
	if anyInUse(&camdet.MicResult{Result: &camdet.Result{}}) {
		t.Fatal("a result with no cameras should not be in use")
	}
}

func TestGetEventInfo(t *testing.T) {
	idle := idleResult()
	info := getEventInfo(idle)
	if info.camerasInUse != 0 || info.micsInUse != 0 {
		t.Fatalf("idle result should have 0 cameras and 0 mics, got %+v", info)
	}

	inUse := inUseResult()
	info = getEventInfo(inUse)
	if info.camerasInUse != 1 || info.micsInUse != 0 {
		t.Fatalf("in-use result should have 1 camera and 0 mics, got %+v", info)
	}
}

func TestEventFor(t *testing.T) {
	tests := []struct {
		name      string
		prev      eventInfo
		curr      eventInfo
		wantEvent string
	}{
		{"all off to mic only", eventInfo{}, eventInfo{micsInUse: 1}, eventMicOnlyOn},
		{"all off to cam only", eventInfo{}, eventInfo{camerasInUse: 1}, eventCamOnlyOn},
		{"all off to both", eventInfo{}, eventInfo{camerasInUse: 1, micsInUse: 1}, eventBothOn},
		{"mic only to all off", eventInfo{micsInUse: 1}, eventInfo{}, eventOff},
		{"cam only to all off", eventInfo{camerasInUse: 1}, eventInfo{}, eventOff},
		{"both to all off", eventInfo{camerasInUse: 1, micsInUse: 1}, eventInfo{}, eventOff},
		{"cam only to both (no transition)", eventInfo{camerasInUse: 1}, eventInfo{camerasInUse: 1, micsInUse: 1}, ""},
		{"both to cam only (no transition)", eventInfo{camerasInUse: 1, micsInUse: 1}, eventInfo{camerasInUse: 1}, ""},
		{"all off to all off", eventInfo{}, eventInfo{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eventFor(tt.prev, tt.curr)
			if got != tt.wantEvent {
				t.Fatalf("eventFor(prev=%+v, curr=%+v) = %q, want %q", tt.prev, tt.curr, got, tt.wantEvent)
			}
		})
	}
}

func TestHookEnvMicOnlyOn(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	// Create a result with only mic in use
	res := &camdet.MicResult{
		Result: &camdet.Result{
			Cameras: []camdet.Camera{{
				Name:      "HD Pro Webcam C920",
				VendorID:  "046d",
				ProductID: "082d",
				Nodes:     []string{"/dev/video0"},
				InUse:     false,
			}},
			FullVisibility: true,
		},
		Mics: []camdet.Mic{{
			Name:    "Blue Yeti",
			Device:  "blueyeti",
			InUse:   true,
			Users:   []camdet.User{{PID: 5678, Name: "chrome"}},
			Backend: "pipewire",
		}},
	}
	got := envMap(hookEnv(eventMicOnlyOn, res, now))

	want := map[string]string{
		"CAMDET_EVENT":          eventMicOnlyOn,
		"CAMDET_TIMESTAMP":      "2026-09-10T12:00:00Z",
		"CAMDET_CAMERAS_IN_USE": "0",
		"CAMDET_MICS_IN_USE":    "1",
		"CAMDET_MIC_NAMES":      "Blue Yeti",
		"CAMDET_MIC_USERS":      "chrome(5678)",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestHookEnvCamOnlyOn(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	got := envMap(hookEnv(eventCamOnlyOn, inUseResult(), now))

	want := map[string]string{
		"CAMDET_EVENT":          eventCamOnlyOn,
		"CAMDET_TIMESTAMP":      "2026-09-10T12:00:00Z",
		"CAMDET_CAMERAS_IN_USE": "1",
		"CAMDET_CAMERA_NAMES":   "HD Pro Webcam C920",
		"CAMDET_CAMERA_IDS":     "046d:082d",
		"CAMDET_USERS":          "zoom(1234)",
		"CAMDET_MICS_IN_USE":    "0",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestHookEnvBothOn(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	// Create a result with both cam and mic in use
	res := &camdet.MicResult{
		Result: &camdet.Result{
			Cameras: []camdet.Camera{{
				Name:      "HD Pro Webcam C920",
				VendorID:  "046d",
				ProductID: "082d",
				Nodes:     []string{"/dev/video0"},
				InUse:     true,
				Streaming: true,
				Users:     []camdet.Process{{PID: 1234, Name: "zoom", Node: "/dev/video0", Streaming: true}},
			}},
			FullVisibility: true,
		},
		Mics: []camdet.Mic{{
			Name:    "Blue Yeti",
			Device:  "blueyeti",
			InUse:   true,
			Users:   []camdet.User{{PID: 5678, Name: "chrome"}},
			Backend: "pipewire",
		}},
	}
	got := envMap(hookEnv(eventBothOn, res, now))

	want := map[string]string{
		"CAMDET_EVENT":          eventBothOn,
		"CAMDET_TIMESTAMP":      "2026-09-10T12:00:00Z",
		"CAMDET_CAMERAS_IN_USE": "1",
		"CAMDET_CAMERA_NAMES":   "HD Pro Webcam C920",
		"CAMDET_CAMERA_IDS":     "046d:082d",
		"CAMDET_USERS":          "zoom(1234)",
		"CAMDET_MICS_IN_USE":    "1",
		"CAMDET_MIC_NAMES":      "Blue Yeti",
		"CAMDET_MIC_USERS":      "chrome(5678)",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestHookEnvOff(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	got := envMap(hookEnv(eventOff, idleResult(), now))

	if got["CAMDET_EVENT"] != eventOff {
		t.Errorf("CAMDET_EVENT = %q, want %q", got["CAMDET_EVENT"], eventOff)
	}
	if got["CAMDET_CAMERAS_IN_USE"] != "0" {
		t.Errorf("CAMDET_CAMERAS_IN_USE = %q, want \"0\"", got["CAMDET_CAMERAS_IN_USE"])
	}
	if got["CAMDET_CAMERA_NAMES"] != "" {
		t.Errorf("CAMDET_CAMERA_NAMES = %q, want empty", got["CAMDET_CAMERA_NAMES"])
	}
	if got["CAMDET_USERS"] != "" {
		t.Errorf("CAMDET_USERS = %q, want empty", got["CAMDET_USERS"])
	}
	if got["CAMDET_MICS_IN_USE"] != "0" {
		t.Errorf("CAMDET_MICS_IN_USE = %q, want \"0\"", got["CAMDET_MICS_IN_USE"])
	}
	if got["CAMDET_MIC_NAMES"] != "" {
		t.Errorf("CAMDET_MIC_NAMES = %q, want empty", got["CAMDET_MIC_NAMES"])
	}
	if got["CAMDET_MIC_USERS"] != "" {
		t.Errorf("CAMDET_MIC_USERS = %q, want empty", got["CAMDET_MIC_USERS"])
	}
}

// TestWatchFiresOnStartupThenOnEdges verifies fire-on-startup semantics and that
// the hook fires only on state changes thereafter, not on every poll.
func TestWatchFiresOnStartupThenOnEdges(t *testing.T) {
	results := []*camdet.MicResult{idleResult(), inUseResult(), inUseResult(), idleResult()}
	index := 0
	detect := func() (*camdet.MicResult, error) {
		r := results[index]
		index++
		return r, nil
	}

	var events []string
	w := &watcher{
		detect: detect,
		onEvent: func(_ context.Context, event string, _ *camdet.MicResult) {
			events = append(events, event)
		},
	}

	tick := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.run(ctx, tick) }()

	tick <- time.Now() // idle (0/0) -> in use cam (1/0): fires cam-only-on
	tick <- time.Now() // cam (1/0) -> cam (1/0): no change
	tick <- time.Now() // cam (1/0) -> idle (0/0): fires off
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	want := []string{"cam-only-on", "off"} // cam-only-on on startup, then off
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestWatchStartupDetectErrorIsFatal(t *testing.T) {
	detect := func() (*camdet.MicResult, error) { return nil, fmt.Errorf("boom") }
	w := &watcher{
		detect:  detect,
		onEvent: func(context.Context, string, *camdet.MicResult) {},
	}
	if err := w.run(context.Background(), make(chan time.Time)); err == nil {
		t.Fatal("expected fatal error when startup detect fails")
	}
}

func TestRunScriptReceivesArgAndEnv(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "out.txt")
	script := filepath.Join(dir, "hook.sh")
	body := "#!/bin/sh\n{ echo \"arg=$1\"; echo \"event=$CAMDET_EVENT\"; echo \"names=$CAMDET_CAMERA_NAMES\"; } > " + outFile + "\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	if err := runScript(context.Background(), script, 5*time.Second, eventCamOnlyOn, inUseResult(), now); err != nil {
		t.Fatalf("runScript: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, want := range []string{"arg=" + eventCamOnlyOn, "event=" + eventCamOnlyOn, "names=HD Pro Webcam C920"} {
		if !strings.Contains(out, want) {
			t.Errorf("hook output missing %q; got:\n%s", want, out)
		}
	}
}

func TestRunScriptTimeoutIsEnforced(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hang.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err := runScript(context.Background(), script, 200*time.Millisecond, eventCamOnlyOn, inUseResult(), time.Now())
	if err == nil {
		t.Fatal("expected a timeout error from a hanging hook")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timeout not enforced; hook ran for %s", elapsed)
	}
}

func TestRunScriptNonZeroExitIsError(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fail.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runScript(context.Background(), script, 5*time.Second, eventOff, idleResult(), time.Now()); err == nil {
		t.Fatal("expected an error when the hook exits non-zero")
	}
}

func TestLogTransitionJSONIsValid(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	logTransition(&buf, eventCamOnlyOn, inUseResult(), now, true)

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("transition JSON is invalid: %v\n%s", err, buf.String())
	}
	if rec["event"] != eventCamOnlyOn {
		t.Errorf("event = %v, want %q", rec["event"], eventCamOnlyOn)
	}
	if rec["timestamp"] != "2026-09-10T12:00:00Z" {
		t.Errorf("timestamp = %v", rec["timestamp"])
	}
}
