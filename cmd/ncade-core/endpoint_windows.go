package main

import (
	"context"
	"errors"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func replaceEndpoint(ctx context.Context, source, destination string) error {
	// Windows can reject replacement while a discovery reader or virus scanner
	// holds either file open. Keep the old endpoint intact and retry briefly.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := os.Rename(source, destination)
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}
