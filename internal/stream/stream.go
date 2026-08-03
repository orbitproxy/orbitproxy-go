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

// Join copies both directions. When either Copy finishes, both ends are Closed.
// Late yamux WindowUpdates after stream removal are benign; yamuxcfg discards
// that library noise from the default log path.
func Join(c1 io.ReadWriteCloser, c2 io.ReadWriteCloser) (inCount int64, outCount int64, errors []error) {
	var wait sync.WaitGroup
	recordErrs := make([]error, 2)

	pipe := func(number int, to io.ReadWriteCloser, from io.ReadWriteCloser, count *int64) {
		defer wait.Done()
		defer to.Close()
		defer from.Close()

		buf := getJoinBuf()
		defer putJoinBuf(buf)
		*count, recordErrs[number] = io.CopyBuffer(to, from, buf)
	}

	wait.Add(2)
	go pipe(0, c1, c2, &inCount)
	go pipe(1, c2, c1, &outCount)
	wait.Wait()

	for _, err := range recordErrs {
		if err != nil {
			errors = append(errors, err)
		}
	}
	return
}
