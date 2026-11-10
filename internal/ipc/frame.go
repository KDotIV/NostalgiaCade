package ipc

import (
	"encoding/binary"
	"fmt"
	"io"
)

const MaxFrameBytes = 1024 * 1024

func ReadFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := binary.LittleEndian.Uint32(header[:])
	if size == 0 || size > MaxFrameBytes {
		return nil, fmt.Errorf("invalid IPC frame size")
	}
	data := make([]byte, int(size))
	_, err := io.ReadFull(r, data)
	return data, err
}

func WriteFrame(w io.Writer, data []byte) error {
	if len(data) == 0 || len(data) > MaxFrameBytes {
		return fmt.Errorf("invalid IPC frame size")
	}
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(data)))
	for _, part := range [][]byte{header[:], data} {
		for len(part) > 0 {
			n, err := w.Write(part)
			if err != nil {
				return err
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			part = part[n:]
		}
	}
	return nil
}
