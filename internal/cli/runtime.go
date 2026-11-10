package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/KdotIV/NostalgiaCade/internal/ipc"
	"github.com/KdotIV/NostalgiaCade/internal/processenv"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type runtimeClient struct{ dataDir string }

// CallLocal lets the operator-side pilot coordinator use the same discovery,
// credential filtering and IPC deadlines as the cabinet CLI.
func CallLocal(ctx context.Context, dataDir, method string, params, result any) error {
	return (runtimeClient{dataDir: dataDir}).call(ctx, method, params, result)
}

func defaultDataDir() string {
	if p := os.Getenv("NCADE_DATA_DIR"); p != "" {
		return p
	}
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "NostalgiaCade")
}
func (r runtimeClient) discover(ctx context.Context) (*ipc.Client, error) {
	p := filepath.Join(r.dataDir, "core-endpoint.json")
	st, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if st.Size() > 4096 {
		return nil, fmt.Errorf("invalid endpoint file")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var ep struct {
		PipeName        string `json:"pipeName"`
		ProtocolVersion int    `json:"protocolVersion"`
	}
	if err = json.Unmarshal(b, &ep); err != nil {
		return nil, err
	}
	if ep.ProtocolVersion != 1 {
		return nil, fmt.Errorf("incompatible running core; close it before updating")
	}
	timeout, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	return ipc.Connect(timeout, ep.PipeName)
}
func (r runtimeClient) call(ctx context.Context, method string, params, result any) error {
	conn, err := r.discover(ctx)
	if err != nil {
		if err = os.MkdirAll(r.dataDir, 0700); err != nil {
			return err
		}
		self, err := os.Executable()
		if err != nil {
			return err
		}
		corePath := os.Getenv("NCADE_CORE_PATH")
		if corePath == "" {
			corePath = filepath.Join(filepath.Dir(self), "ncade-core.exe")
		}
		log, err := os.OpenFile(filepath.Join(r.dataDir, "core.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		cmd := exec.Command(corePath, "--data-dir", r.dataDir)
		cmd.Env = processenv.Cabinet(os.Environ())
		cmd.Stdout = log
		cmd.Stderr = log
		hideConsole(cmd)
		if err = cmd.Start(); err != nil {
			log.Close()
			return err
		}
		_ = cmd.Process.Release()
		log.Close()
		startup, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		for {
			conn, err = r.discover(startup)
			if err == nil {
				break
			}
			select {
			case <-startup.Done():
				return fmt.Errorf("core startup failed; inspect %s", filepath.Join(r.dataDir, "core.log"))
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	defer conn.Close()
	return conn.Call(ctx, method, params, result)
}
