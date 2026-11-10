package ipc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/KdotIV/NostalgiaCade/internal/core"
	"github.com/KdotIV/NostalgiaCade/internal/database"
)

type fragmentedReader struct{ *bytes.Reader }

func (r fragmentedReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}

func TestFramesHandleFragmentationAndRejectInvalidSizes(t *testing.T) {
	var out bytes.Buffer
	if err := WriteFrame(&out, []byte(`{"id":1}`)); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFrame(fragmentedReader{bytes.NewReader(out.Bytes())})
	if err != nil || string(got) != `{"id":1}` {
		t.Fatalf("%s %v", got, err)
	}
	for _, size := range []uint32{0, MaxFrameBytes + 1, ^uint32(0)} {
		var header [4]byte
		binary.LittleEndian.PutUint32(header[:], size)
		if _, err := ReadFrame(bytes.NewReader(header[:])); err == nil {
			t.Fatalf("accepted %d", size)
		}
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{10, 0, 0, 0, 1})); err == nil {
		t.Fatal("accepted truncated body")
	}
}

func TestHandshakeAndRPC(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db, err := database.OpenSQLite(ctx, filepath.Join(t.TempDir(), "ipc.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migration, err := database.Migrate(ctx, db, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{Service: &core.Service{DB: db, DatabaseVersion: migration.Version, StartedAt: time.Now()}}
	client, peer := net.Pipe()
	defer client.Close()
	go server.serveConn(ctx, peer)
	exchange := func(payload string) Response {
		t.Helper()
		client.SetDeadline(time.Now().Add(2 * time.Second))
		if err := WriteFrame(client, []byte(payload)); err != nil {
			t.Fatal(err)
		}
		data, err := ReadFrame(client)
		if err != nil {
			t.Fatal(err)
		}
		var response Response
		if err := json.Unmarshal(data, &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	if r := exchange(`{"jsonrpc":"2.0","id":"a","method":"system.status"}`); r.Error == nil || r.Error.Code != -32002 {
		t.Fatalf("missing handshake: %+v", r)
	}
	if r := exchange(`{"jsonrpc":"2.0","id":"b","method":"system.hello","params":{"protocolVersion":99}}`); r.Error == nil || r.Error.Code != -32001 {
		t.Fatalf("bad version: %+v", r)
	}
	if r := exchange(`{"jsonrpc":"2.0","id":"c","method":"system.hello","params":{"protocolVersion":1}}`); r.Error != nil {
		t.Fatalf("hello: %+v", r)
	}
	if r := exchange(`{"jsonrpc":"2.0","id":"d","method":"catalog.genres"}`); r.Error != nil || len(r.Result.([]any)) != 5 {
		t.Fatalf("genres: %+v", r)
	}
	if r := exchange(`{"jsonrpc":"2.0","id":"e","method":"catalog.addGame"}`); r.Error == nil || r.Error.Code != -32601 {
		t.Fatalf("catalog write exposed: %+v", r)
	}
	if r := exchange(`{"jsonrpc":"2.0","id":null,"method":"system.status"}`); r.Error == nil || r.Error.Code != -32600 {
		t.Fatalf("null request ID accepted: %+v", r)
	}
}
