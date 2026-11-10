//go:build windows

package ipc

import (
	"context"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func Listen(name string) (net.Listener, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("read current Windows identity: %w", err)
	}
	// go-winio also sets FILE_PIPE_REJECT_REMOTE_CLIENTS. Grant only the
	// current user, not all administrators, authenticated users or Everyone.
	sddl := "D:P(A;;GA;;;" + user.User.Sid.String() + ")"
	return winio.ListenPipe(`\\.\pipe\`+name, &winio.PipeConfig{
		SecurityDescriptor: sddl, InputBufferSize: 65536, OutputBufferSize: 65536,
	})
}

func dialPipe(ctx context.Context, name string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, `\\.\pipe\`+name)
}
