package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/syberfoxx/gamehost/internal/backup"
	"github.com/syberfoxx/gamehost/internal/instance"
	"github.com/syberfoxx/gamehost/internal/process"
	"github.com/syberfoxx/gamehost/internal/store"
)

const (
	// backupTick is how often the schedule looks for a due backup.
	backupTick = 30 * time.Second
	// lockWait is how long a scheduled backup waits for the instance lock after
	// stopping the server, so the supervisor can finish its final state write.
	lockWait = 10 * time.Second
)

// backupScheduler runs the snapshots an instance schedules for itself.
// A due backup stops the server first, because Core Keeper rewrites its world in
// place and a live archive can catch a half-written save. A running server is
// started again once the archive and the retention are done.
type backupScheduler struct {
	store   *store.Store
	out     io.Writer
	started time.Time
	now     func() time.Time
	stop    func(instance.Instance) error
	start   func(instance.Instance) error
}

// newBackupScheduler returns the scheduler used by serve.
func newBackupScheduler(s *store.Store, out io.Writer) *backupScheduler {
	return &backupScheduler{
		store:   s,
		out:     out,
		started: time.Now(),
		now:     time.Now,
		stop: func(inst instance.Instance) error {
			return stopInstance(s, inst, out)
		},
		start: func(inst instance.Instance) error {
			return startInstance(s, inst, out)
		},
	}
}

// Run checks every instance until ctx ends.
func (b *backupScheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(backupTick)
	defer ticker.Stop()
	for {
		b.check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// check runs the backups that are due, reporting failures without stopping.
func (b *backupScheduler) check(ctx context.Context) {
	instances, err := b.store.List()
	if err != nil {
		fmt.Fprintf(b.out, "scheduled backups: %v\n", err)
		return
	}
	for _, inst := range instances {
		if ctx.Err() != nil {
			return
		}
		if err := b.maybeBackup(inst); err != nil {
			fmt.Fprintf(b.out, "scheduled backup of %s: %v\n", inst.Name, err)
		}
	}
}

// maybeBackup archives inst when its interval has elapsed.
func (b *backupScheduler) maybeBackup(inst instance.Instance) error {
	every := inst.Backup.Interval()
	if every == 0 {
		return nil
	}

	last := b.started
	newest, ok, err := backup.Newest(b.store.Layout(inst.Name).Backups)
	if err != nil {
		return err
	}
	if ok {
		last = newest
	}
	if b.now().Sub(last) < every {
		return nil
	}

	state, err := b.store.LoadState(inst.Name)
	if err != nil {
		return err
	}
	running := state.Running() && process.Alive(state.PID)
	if running {
		fmt.Fprintf(b.out, "scheduled backup of %s: stopping the server for a clean archive\n", inst.Name)
		if err := b.stop(inst); err != nil {
			return err
		}
	}

	archiveErr := b.snapshot(inst)

	// A failed backup must not leave the server down.
	if running {
		if err := b.start(inst); err != nil {
			if archiveErr != nil {
				return fmt.Errorf("%w; restart after the backup: %w", archiveErr, err)
			}
			return fmt.Errorf("restart after the backup: %w", err)
		}
	}
	return archiveErr
}

// snapshot archives the data of a stopped instance and applies its retention.
func (b *backupScheduler) snapshot(inst instance.Instance) error {
	state, err := b.store.LoadState(inst.Name)
	if err != nil {
		return err
	}
	if process.Alive(state.PID) {
		return fmt.Errorf("the server is still running, backup skipped")
	}

	lock, err := lockInstance(b.store, inst)
	if err != nil {
		return err
	}
	res, err := archiveData(b.store, inst, "")
	if err == nil && res.Path != "" {
		var removed []string
		removed, err = backup.Prune(b.store.Layout(inst.Name).Backups, inst.Backup.Retention())
		for _, name := range removed {
			fmt.Fprintf(b.out, "scheduled backup of %s: removed old backup %s\n", inst.Name, name)
		}
	}
	if releaseErr := lock.Release(); err == nil {
		err = releaseErr
	}
	if err != nil {
		return err
	}
	if res.Path == "" {
		fmt.Fprintf(b.out, "scheduled backup of %s skipped: no data yet\n", inst.Name)
		return nil
	}

	fmt.Fprintf(b.out, "scheduled backup of %s: %s (%d files, %s)\n",
		inst.Name, res.Path, res.Files, humanBytes(res.Bytes))
	return nil
}

// lockInstance waits for the run lock, which a supervisor may hold for a moment
// after it recorded the final state.
func lockInstance(s *store.Store, inst instance.Instance) (*store.Lock, error) {
	deadline := time.Now().Add(lockWait)
	for {
		lock, err := s.Lock(inst.Name)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, store.ErrLocked) || time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}
