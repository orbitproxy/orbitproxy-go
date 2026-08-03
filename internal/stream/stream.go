package stream

import (
	"io"
	"sync"
)

const joinBufferSize = 16 * 1024

var joinBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, joinBufferSize)
		return &buf
	},
}

func getJoinBuf() []byte {
	return *(joinBufPool.Get().(*[]byte))
}

func putJoinBuf(buf []byte) {
	if cap(buf) < joinBufferSize {
		return
	}
	buf = buf[:joinBufferSize]
	joinBufPool.Put(&buf)
}

// WorkConnWrapOptions configures optional work-conn transforms.
type WorkConnWrapOptions struct {
	EncryptionKey  []byte
	UseEncryption  bool
	UseCompression bool
	Limiter        any
}

// WrapWorkConn currently returns the connection unchanged.
func WrapWorkConn(conn io.ReadWriteCloser, opt WorkConnWrapOptions) (io.ReadWriteCloser, func(), error) {
	if conn == nil {
		return nil, func() {}, nil
	}
	_ = opt
	return conn, func() {}, nil
}

type closeWriter interface {
	CloseWrite() error
}

// closeWriteSide half-closes the write side after one Copy direction finishes.
// TCPConn implements CloseWrite; yamux.Stream.Close sends FIN (local half-close).
func closeWriteSide(c io.ReadWriteCloser) {
	if cw, ok := c.(closeWriter); ok {
		_ = cw.CloseWrite()
		return
	}
	_ = c.Close()
}

// closeFullyAfterJoin releases FDs that used CloseWrite (e.g. *net.TCPConn).
// Do not Close again for types where Close is already half-close (yamux.Stream):
// a second Close while still local-closed force-removes the stream and can
// truncate an in-flight HTTP response body.
func closeFullyAfterJoin(c io.ReadWriteCloser) {
	if _, ok := c.(closeWriter); ok {
		_ = c.Close()
	}
}

// Join copies both directions with half-close semantics:
// when A→B copy ends, only B's write side is closed so B→A can finish.
func Join(c1 io.ReadWriteCloser, c2 io.ReadWriteCloser) (inCount int64, outCount int64, errors []error) {
	var wait sync.WaitGroup
	recordErrs := make([]error, 2)

	pipe := func(number int, dst io.ReadWriteCloser, src io.ReadWriteCloser, count *int64) {
		defer wait.Done()

		buf := getJoinBuf()
		defer putJoinBuf(buf)
		*count, recordErrs[number] = io.CopyBuffer(dst, src, buf)
		closeWriteSide(dst)
	}

	wait.Add(2)
	go pipe(0, c1, c2, &inCount)
	go pipe(1, c2, c1, &outCount)
	wait.Wait()

	closeFullyAfterJoin(c1)
	closeFullyAfterJoin(c2)

	for _, err := range recordErrs {
		if err != nil {
			errors = append(errors, err)
		}
	}
	return
}
