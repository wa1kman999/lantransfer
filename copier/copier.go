package copier

import (
	"context"
	"errors"
	"io"
)

var IoCopyCancelledErr = errors.New("io copy cancelled err")

type readerFunc func(p []byte) (n int, err error)

func (rf readerFunc) Read(p []byte) (n int, err error) { return rf(p) }

// Copy closable copy
func CopyN(ctx context.Context, dst io.Writer, src io.Reader, n int64) (int64, error) {
	// Copy will call the Reader and Writer interface multiple time, in order
	// to copy by chunk (avoiding loading the whole file in memory).
	// I insert the ability to cancel before read time as it is the earliest
	// possible in the call process.
	size, err := io.CopyN(dst, readerFunc(func(p []byte) (int, error) {
		select {
		// if context has been canceled
		case <-ctx.Done():
			// stop process and propagate "context canceled" error
			return 0, IoCopyCancelledErr
		default:
			// otherwise just run default io.Reader implementation
			return src.Read(p)
		}
	}), n)
	return size, err
}
