//go:build !windows

package ipc

import (
	"context"
	"fmt"
	"net"
)

func dialPipe(ctx context.Context, name string) (net.Conn, error) {
	return nil, fmt.Errorf("named pipes require Windows")
}

func Listen(name string) (net.Listener, error) {
	return nil, fmt.Errorf("desktop IPC currently supports Windows only")
}
