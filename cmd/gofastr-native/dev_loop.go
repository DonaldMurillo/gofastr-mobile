package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// errStopDev ends the loop for a condition a rebuild cannot fix, such as
// a signing mismatch that only a manual, data-deleting uninstall resolves.
var errStopDev = errors.New("dev stopped")

// devLoop schedules rebuilds. It has no file system, clock, or process
// access of its own, so the coalescing rules are tested with fakes:
//
//   - a change starts a settle period; it ends after settle with no
//     further change, or after maxSettle, so a file touched constantly
//     cannot starve builds;
//   - the snapshot taken just before a build is the new baseline, so any
//     number of edits during that build cause exactly one follow-up build;
//   - nothing interrupts a running build except ctx (Ctrl+C);
//   - a failed iteration is reported and the loop keeps watching.
type devLoop struct {
	poll, settle, maxSettle time.Duration
	now                     func() time.Time
	sleep                   func(context.Context, time.Duration) error
	scan                    func() map[string]stamp
	// build runs one iteration. Its error is printed, except errStopDev,
	// which ends the loop.
	build func(context.Context) error
	// refresh, if set, recomputes the watch set after an iteration and
	// carries the build-start snapshot over to it.
	refresh func(base map[string]stamp) map[string]stamp
	out     io.Writer
}

func (l *devLoop) Run(ctx context.Context) error {
	base := l.scan()
	if err := l.iterate(ctx, &base); err != nil {
		return err
	}
	for {
		current := l.scan()
		if sameFingerprint(current, base) {
			if err := l.sleep(ctx, l.poll); err != nil {
				return err
			}
			continue
		}
		start := l.now()
		quietSince, last := start, current
		for l.now().Sub(quietSince) < l.settle && l.now().Sub(start) < l.maxSettle {
			if err := l.sleep(ctx, l.poll); err != nil {
				return err
			}
			if next := l.scan(); !sameFingerprint(next, last) {
				last, quietSince = next, l.now()
			}
		}
		next := l.scan()
		fmt.Fprintf(l.out, "\nchanged: %s\n", strings.Join(changedPaths(base, next, 5), ", "))
		base = next
		if err := l.iterate(ctx, &base); err != nil {
			return err
		}
	}
}

func (l *devLoop) iterate(ctx context.Context, base *map[string]stamp) error {
	err := l.build(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, errStopDev) {
		return err
	}
	if err != nil {
		fmt.Fprintf(l.out, "\n%v\n", err)
	}
	if l.refresh != nil {
		*base = l.refresh(*base)
	}
	fmt.Fprintln(l.out, "\nwatching for changes (Ctrl+C stops)")
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
