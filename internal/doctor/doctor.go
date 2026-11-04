// Package doctor probes the environment needed to run game servers.
package doctor

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"text/tabwriter"

	"gamehost/internal/steamcmd"
)

// Status is the outcome of one environment check.
type Status int

const (
	// OK means the check passed.
	OK Status = iota
	// Warn means the check found something worth knowing but not blocking.
	Warn
	// Fail means a required condition is not met.
	Fail
)

// String returns the label printed in the report.
func (s Status) String() string {
	switch s {
	case OK:
		return "ok"
	case Warn:
		return "warn"
	case Fail:
		return "fail"
	default:
		return "unknown"
	}
}

// Check is the result of one environment probe.
type Check struct {
	Name     string
	Status   Status
	Detail   string
	Required bool
}

// Report is the full result of a doctor run.
type Report struct {
	Root   string
	Checks []Check
}

// Healthy reports whether every required check passed.
func (r Report) Healthy() bool {
	for _, c := range r.Checks {
		if c.Required && c.Status == Fail {
			return false
		}
	}
	return true
}

// Run probes the environment rooted at root.
func Run(root string) Report {
	return Report{
		Root: root,
		Checks: []Check{
			platform(runtime.GOOS, runtime.GOARCH),
			rootWritable(root),
			checkSteamcmd(steamcmd.Locate),
			display(os.Getenv),
		},
	}
}

// Format writes the report as an aligned table.
func Format(w io.Writer, r Report) error {
	if _, err := fmt.Fprintf(w, "root: %s\n\n", r.Root); err != nil {
		return fmt.Errorf("write report header: %w", err)
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, c := range r.Checks {
		if _, err := fmt.Fprintf(tw, "  %s\t%s\t%s\n", c.Status, c.Name, c.Detail); err != nil {
			return fmt.Errorf("format check %q: %w", c.Name, err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("flush report: %w", err)
	}

	summary := "environment ready"
	if !r.Healthy() {
		summary = "environment is not ready"
	}
	if _, err := fmt.Fprintf(w, "\n%s\n", summary); err != nil {
		return fmt.Errorf("write report summary: %w", err)
	}
	return nil
}

// platform rejects operating systems the process supervisor cannot support.
func platform(goos, goarch string) Check {
	c := Check{Name: "platform", Required: true, Detail: goos + "/" + goarch}
	if goos != "linux" {
		c.Status = Fail
		c.Detail += " (Linux only)"
		return c
	}
	c.Status = OK
	return c
}

// rootWritable creates and removes a probe file under root.
func rootWritable(root string) Check {
	c := Check{Name: "data root", Required: true, Detail: root}
	if err := os.MkdirAll(root, 0o755); err != nil {
		c.Status = Fail
		c.Detail = err.Error()
		return c
	}

	probe, err := os.CreateTemp(root, ".doctor-*")
	if err != nil {
		c.Status = Fail
		c.Detail = "not writable: " + err.Error()
		return c
	}
	name := probe.Name()
	_ = probe.Close()   // the probe already proved the directory is writable
	_ = os.Remove(name) // best effort; a leftover file is harmless

	c.Status = OK
	return c
}

// checkSteamcmd resolves SteamCMD through the injected locator.
func checkSteamcmd(locate func() (string, error)) Check {
	c := Check{Name: "steamcmd", Required: true}
	path, err := locate()
	if err != nil {
		c.Status = Fail
		c.Detail = "not found (set " + steamcmd.EnvPath + " or install the official tarball)"
		return c
	}
	c.Status = OK
	c.Detail = path
	return c
}

// display reports whether an X display is available for the Unity server.
func display(getenv func(string) string) Check {
	c := Check{Name: "display"}
	if value := getenv("DISPLAY"); value != "" {
		c.Status = OK
		c.Detail = value
		return c
	}
	c.Status = Warn
	c.Detail = "DISPLAY is not set; the server may need a display"
	return c
}
