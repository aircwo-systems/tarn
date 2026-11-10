package engine

import (
	"encoding/binary"
	"io"

	"github.com/moby/moby/api/pkg/stdcopy"
)

// dockerStreamWriter emits Docker's eight-byte multiplexed log frame header.
type dockerStreamWriter struct {
	output io.Writer
	stream stdcopy.StdType
}

func newDockerStreamWriter(output io.Writer, stream stdcopy.StdType) io.Writer {
	return dockerStreamWriter{output: output, stream: stream}
}
func (w dockerStreamWriter) Write(data []byte) (int, error) {
	header := make([]byte, 8)
	header[0] = byte(w.stream)
	binary.BigEndian.PutUint32(header[4:], uint32(len(data)))
	if _, err := w.output.Write(header); err != nil {
		return 0, err
	}
	return w.output.Write(data)
}
