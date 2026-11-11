//go:build !windows

package main

import (
	"context"
	"os"
)

func replaceEndpoint(ctx context.Context, source, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(source, destination)
}
