package app

import (
	"fmt"
	"io"

	"github.com/syberfoxx/gamehost/internal/doctor"
	"github.com/syberfoxx/gamehost/internal/home"
)

// runDoctor prints the environment report and fails when it is not ready.
func runDoctor(stdout io.Writer) error {
	root, err := home.Dir()
	if err != nil {
		return fmt.Errorf("resolve data directory: %w", err)
	}

	report := doctor.Run(root)
	if err := doctor.Format(stdout, report); err != nil {
		return fmt.Errorf("write doctor report: %w", err)
	}
	if !report.Healthy() {
		return ErrUnhealthy
	}
	return nil
}
