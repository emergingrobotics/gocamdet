// Command gocamdet reports which USB cameras are currently in use on this
// Linux system. It prints a human-readable table by default, or JSON with
// --json, and sets its exit code so it can be used in shell scripts:
//
//	0  no camera streaming and no mic in use
//	1  a camera is actively streaming, or a mic is in use
//	2  error
//
// A camera that is merely open (e.g. an app probing device capabilities) is
// reported in the table but is not "streaming" and does not set exit code 1.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gherlein/gocamdet"
)

const (
	exitNoneInUse = 0
	exitInUse     = 1
	exitError     = 2
)

func main() {
	os.Exit(run())
}

func run() int {
	asJSON := flag.Bool("json", false, "emit the result as JSON on stdout")
	watch := flag.Bool("watch", false, "run continuously, firing a hook on camera/mic on/off transitions")
	interval := flag.Duration("interval", 2*time.Second, "poll interval in --watch mode")
	script := flag.String("script", "", "hook script run on each transition in --watch mode (receives mic-only-on|cam-only-on|both-on|off)")
	scriptTimeout := flag.Duration("script-timeout", 30*time.Second, "maximum run time for a --watch hook script")
	flag.Parse()

	if *watch {
		return runWatch(*script, *interval, *scriptTimeout, *asJSON)
	}

	result, err := camdet.DetectWithMic()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gocamdet: %v\n", err)
		return exitError
	}

	if !result.FullVisibility {
		fmt.Fprintln(os.Stderr, "gocamdet: reduced visibility — not all processes could be inspected; re-run with sudo for complete results")
	}

	if *asJSON {
		if err := emitJSON(result); err != nil {
			fmt.Fprintf(os.Stderr, "gocamdet: %v\n", err)
			return exitError
		}
	} else {
		emitTable(result)
	}

	return exitCode(result)
}

func emitJSON(result *camdet.MicResult) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func emitTable(result *camdet.MicResult) {
	if len(result.Cameras) == 0 && len(result.Mics) == 0 {
		fmt.Println("No USB cameras or microphones found.")
		return
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "DEVICE\tTYPE\tNODES\tOPEN\tSTREAMING\tUSED BY")

	// Emit cameras: OPEN means a process holds a node; STREAMING means one is
	// actively capturing (device mmap'd). A probe shows OPEN=yes, STREAMING=no.
	for _, cam := range result.Cameras {
		fmt.Fprintf(writer, "%s\tcamera\t%s\t%s\t%s\t%s\n",
			orDash(cam.Name),
			strings.Join(cam.Nodes, ","),
			yesNo(cam.InUse),
			yesNo(cam.Streaming),
			formatUsers(cam.Users),
		)
	}

	// Emit microphones. A mic's "in use" already means an active capture stream
	// (from /proc/asound status), so it has no separate open-vs-streaming state.
	for _, mic := range result.Mics {
		fmt.Fprintf(writer, "%s\tmic\t%s\t%s\t%s\t%s\n",
			orDash(mic.Name),
			mic.Device,
			yesNo(mic.InUse),
			"-",
			formatMicUsers(mic.Users),
		)
	}
	_ = writer.Flush()
}

func exitCode(result *camdet.MicResult) int {
	for _, cam := range result.Cameras {
		if cam.Streaming {
			return exitInUse
		}
	}
	for _, mic := range result.Mics {
		if mic.InUse {
			return exitInUse
		}
	}
	return exitNoneInUse
}

func formatMicUsers(users []camdet.User) string {
	if len(users) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(users))
	for _, u := range users {
		parts = append(parts, fmt.Sprintf("%s(%d)", u.Name, u.PID))
	}
	return strings.Join(parts, ",")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
