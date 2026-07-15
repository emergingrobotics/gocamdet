// Command gocamdet reports which USB cameras are currently in use on this
// Linux system. It prints a human-readable table by default, or JSON with
// --json, and sets its exit code so it can be used in shell scripts:
//
//	0  no camera in use
//	1  at least one camera in use
//	2  error
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

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
	flag.Parse()

	result, err := camdet.Detect()
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

func emitJSON(result *camdet.Result) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func emitTable(result *camdet.Result) {
	if len(result.Cameras) == 0 {
		fmt.Println("No USB cameras found.")
		return
	}

	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(writer, "CAMERA\tVENDOR:PRODUCT\tNODES\tIN USE\tUSED BY")
	for _, cam := range result.Cameras {
		fmt.Fprintf(writer, "%s\t%s:%s\t%s\t%s\t%s\n",
			orDash(cam.Name),
			cam.VendorID, cam.ProductID,
			strings.Join(cam.Nodes, ","),
			yesNo(cam.InUse),
			formatUsers(cam.Users),
		)
	}
	_ = writer.Flush()
}

func formatUsers(users []camdet.Process) string {
	if len(users) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(users))
	for _, u := range users {
		parts = append(parts, fmt.Sprintf("%s(%s)", u.Name, strconv.Itoa(u.PID)))
	}
	return strings.Join(parts, ",")
}

func exitCode(result *camdet.Result) int {
	for _, cam := range result.Cameras {
		if cam.InUse {
			return exitInUse
		}
	}
	return exitNoneInUse
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
