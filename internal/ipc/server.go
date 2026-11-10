package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/KdotIV/NostalgiaCade/internal/core"
	"github.com/KdotIV/NostalgiaCade/internal/library"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type Server struct {
	Service  *core.Service
	Shutdown context.CancelFunc
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	var clients sync.WaitGroup
	defer func() { cancel(); clients.Wait() }()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	limit := make(chan struct{}, 16)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case limit <- struct{}{}:
			clients.Add(1)
			go func() { defer clients.Done(); defer func() { <-limit }(); s.serveConn(ctx, conn) }()
		default:
			_ = conn.Close()
		}
	}
}

func (s *Server) serveConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	handshaken := false
	for {
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		if !handshaken {
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		}
		data, err := ReadFrame(conn)
		if err != nil {
			return
		}
		var req Request
		response := Response{JSONRPC: "2.0", ID: json.RawMessage("null")}
		shutdown := false
		if json.Unmarshal(data, &req) != nil {
			response.Error = &Error{-32700, "Invalid JSON request"}
		} else if req.JSONRPC != "2.0" || !validID(req.ID) || req.Method == "" {
			response.Error = &Error{-32600, "Expected a JSON-RPC request with a string or integer ID"}
		} else {
			response.ID = req.ID
			callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			response.Result, response.Error, shutdown = s.dispatch(callCtx, req, &handshaken)
			cancel()
		}
		// Once shutdown admission is reserved, also stop if the client loses its
		// connection before receiving the acknowledgement.
		if shutdown && s.Shutdown != nil {
			defer s.Shutdown()
		}
		out, err := json.Marshal(response)
		if err != nil {
			return
		}
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := WriteFrame(conn, out); err != nil {
			return
		}
		if shutdown && s.Shutdown != nil {
			return
		}
	}
}

func validID(id json.RawMessage) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	var text string
	if json.Unmarshal(id, &text) == nil {
		return text != ""
	}
	var n int64
	return json.Unmarshal(id, &n) == nil
}

func (s *Server) dispatch(ctx context.Context, req Request, handshaken *bool) (any, *Error, bool) {
	if req.Method == "system.hello" {
		var params struct {
			ProtocolVersion int `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &params) != nil {
			return nil, &Error{-32602, "protocolVersion is required"}, false
		}
		if params.ProtocolVersion != core.ProtocolVersion {
			return nil, &Error{-32001, "Incompatible protocol version"}, false
		}
		*handshaken = true
		return map[string]any{"protocolVersion": core.ProtocolVersion, "coreVersion": core.Version}, nil, false
	}
	if !*handshaken {
		return nil, &Error{-32002, "Call system.hello first"}, false
	}
	var result any
	var err error
	switch req.Method {
	case "system.status":
		result, err = s.Service.Status(ctx)
	case "catalog.genres":
		result, err = s.Service.Genres(ctx)
	case "system.shutdown":
		if s.Service.Library != nil {
			if err := s.Service.Library.PrepareShutdown(); err != nil {
				return nil, &Error{-32010, err.Error()}, false
			}
		}
		return map[string]bool{"accepted": true}, nil, true
	default:
		return s.libraryCall(ctx, req)
	}
	if err != nil {
		return nil, &Error{-32603, "Local database operation failed"}, false
	}
	return result, nil, false
}

func decodeParams(data json.RawMessage, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func (s *Server) libraryCall(ctx context.Context, req Request) (any, *Error, bool) {
	m := s.Service.Library
	if m == nil {
		return nil, &Error{-32601, "Method not found"}, false
	}
	var result any
	var err error
	bad := func() (any, *Error, bool) { return nil, &Error{-32602, "Invalid method parameters"}, false }
	var id struct {
		ID string `json:"id"`
	}
	switch req.Method {
	case "setup.status":
		result, err = m.SetupStatus(ctx)
	case "setup.configure":
		var v library.Setup
		if decodeParams(req.Params, &v) != nil {
			return bad()
		}
		result, err = m.Configure(ctx, v)
	case "emulators.register":
		var v library.Emulator
		if decodeParams(req.Params, &v) != nil {
			return bad()
		}
		result, err = m.RegisterEmulator(ctx, v)
	case "emulators.installed":
		result, err = m.InstalledEmulators(ctx)
	case "emulators.profile":
		if decodeParams(req.Params, &id) != nil {
			return bad()
		}
		result, err = m.ProfileStatus(ctx, id.ID)
	case "emulators.configure":
		var v library.ConfigureEmulatorRequest
		if decodeParams(req.Params, &v) != nil {
			return bad()
		}
		result, err = m.ConfigureEmulator(ctx, v)
	case "library.games":
		result, err = m.Games(ctx)
	case "library.plan":
		var v library.Manifest
		if decodeParams(req.Params, &v) != nil {
			return bad()
		}
		result, err = m.Plan(ctx, v)
	case "operations.submit":
		var v library.Request
		if decodeParams(req.Params, &v) != nil {
			return bad()
		}
		result, err = m.Submit(ctx, v)
	case "operations.get", "operations.retry", "operations.cancel":
		if decodeParams(req.Params, &id) != nil {
			return bad()
		}
		if req.Method == "operations.get" {
			result, err = m.Operation(ctx, id.ID)
		} else if req.Method == "operations.retry" {
			result, err = m.Retry(ctx, id.ID)
		} else {
			err = m.Cancel(ctx, id.ID)
			result = map[string]bool{"accepted": err == nil}
		}
	case "session.launch":
		if decodeParams(req.Params, &id) != nil {
			return bad()
		}
		result, err = m.Launch(ctx, id.ID)
	case "session.status":
		result = m.SessionStatus()
	case "session.stop":
		var v struct {
			Force             bool   `json:"force"`
			ExpectedSessionID string `json:"expectedSessionId,omitempty"`
		}
		if decodeParams(req.Params, &v) != nil {
			return bad()
		}
		err = m.StopSession(v.Force, v.ExpectedSessionID)
		result = map[string]bool{"accepted": err == nil}
	case "session.acknowledge":
		var v struct {
			Confirmed bool `json:"confirmed"`
		}
		if decodeParams(req.Params, &v) != nil {
			return bad()
		}
		err = m.AcknowledgeSession(ctx, v.Confirmed)
		result = map[string]bool{"accepted": err == nil}
	default:
		return nil, &Error{-32601, "Method not found"}, false
	}
	if err != nil {
		var fault *library.Fault
		if errors.As(err, &fault) {
			return nil, &Error{-32010, fault.Error()}, false
		}
		slog.Error("local request failed", "method", req.Method, "error", err)
		return nil, &Error{-32603, "Local operation failed; check paths, file access and core logs"}, false
	}
	return result, nil, false
}
