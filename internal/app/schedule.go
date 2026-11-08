package app

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/syberfoxx/gamehost/internal/backup"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/store"
)

// backupTick is how often the schedule looks for a due backup.
const backupTick = 30 * time.Second

// backupDue reports whether inst wants its maintenance window now.
// The reference is the newest archive, or the start of the current run when
// there is none, so a restart after a window counts from that archive and a
// failed one still waits a full interval before trying again.
func backupDue(s *store.Store, inst instance.Instance, started, now time.Time) bool {
	every := inst.Backup.Interval()
	if every == 0 {
		return false
	}

	last := started
	if newest, ok, err := backup.Newest(s.Layout(inst.Name).Backups); err == nil && ok && newest.After(last) {
		last = newest
	}
	return !now.Before(last.Add(every))
}

// archiveWindow archives a stopped instance and applies the retention of its
// schedule. The server is already down: the supervisor stops it before the
// archive, because the games rewrite their world in place.
func archiveWindow(s *store.Store, inst instance.Instance, out io.Writer) error {
	res, err := archiveData(s, inst, "")
	if err != nil {
		return err
	}
	if res.Path == "" {
		fmt.Fprintf(out, "scheduled backup of %s skipped: no data yet\n", inst.Name)
		return nil
	}

	removed, err := backup.Prune(s.Layout(inst.Name).Backups, inst.Backup.Retention())
	for _, name := range removed {
		fmt.Fprintf(out, "scheduled backup of %s: removed old backup %s\n", inst.Name, name)
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "scheduled backup of %s: %s (%d files, %s)\n",
		inst.Name, res.Path, res.Files, humanBytes(res.Bytes))
	return nil
}

// watchSchedule signals due once when the instance wants a window, then stops.
// It reloads the instance on every tick, so a schedule edited while the server
// runs takes effect without a restart.
func watchSchedule(ctx context.Context, s *store.Store, name string, started time.Time, due chan<- struct{}, done <-chan struct{}) {
	ticker := time.NewTicker(backupTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
		}

		inst, err := s.Load(name)
		if err != nil {
			continue
		}
		if !backupDue(s, inst, started, time.Now()) {
			continue
		}

		select {
		case due <- struct{}{}:
		default:
		}
		return
	}
}
