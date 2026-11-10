package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sync"
	"time"
)

type Client struct {
	conn     net.Conn
	mu       sync.Mutex
	sequence int
}

var pipePattern = regexp.MustCompile(`^NostalgiaCade\.[a-f0-9]{32}$`)

func Connect(ctx context.Context, name string) (*Client, error) {
	if !pipePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid endpoint")
	}
	conn, err := dialPipe(ctx, name)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn}
	var hello struct {
		ProtocolVersion int `json:"protocolVersion"`
	}
	if err = c.Call(ctx, "system.hello", map[string]int{"protocolVersion": 1}, &hello); err != nil {
		conn.Close()
		return nil, err
	}
	if hello.ProtocolVersion != 1 {
		conn.Close()
		return nil, fmt.Errorf("incompatible core")
	}
	return c, nil
}
func (c *Client) Close() error { return c.conn.Close() }
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	c.sequence++
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	c.conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { c.conn.Close() })
	defer stop()
	id := fmt.Sprint(c.sequence)
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if err = WriteFrame(c.conn, body); err != nil {
		c.conn.Close()
		return err
	}
	data, err := ReadFrame(c.conn)
	if err != nil {
		c.conn.Close()
		return err
	}
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *Error          `json:"error"`
	}
	if err = json.Unmarshal(data, &response); err != nil || response.JSONRPC != "2.0" || response.ID != id {
		c.conn.Close()
		return fmt.Errorf("invalid core response")
	}
	if response.Error != nil {
		return fmt.Errorf("%s", response.Error.Message)
	}
	if result != nil {
		return json.Unmarshal(response.Result, result)
	}
	return nil
}
