// Package tail follows an append-only file and reopens it after rotation.
package tail

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Follow skips existing bytes on the first open, but starts replacement files at byte zero.
func Follow(ctx context.Context, path string, ready func(), emit func(string) error) error {
	var file *os.File
	defer func() {
		if file != nil {
			file.Close()
		}
	}()
	wake := notifications(ctx, filepath.Dir(path))
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var offset int64
	var pending strings.Builder
	var oversized bool
	first := true
	var reader *bufio.Reader
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if file == nil {
			opened, err := os.Open(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err == nil {
				file = opened
				if first {
					offset, err = file.Seek(0, io.SeekEnd)
					if err != nil {
						return err
					}
					// When starting inside a partial line, skip its remaining bytes.
					if offset > 0 {
						var last [1]byte
						if _, err = file.ReadAt(last[:], offset-1); err != nil {
							return err
						}
						oversized = last[0] != '\n'
					}
					first = false
					ready()
				}
				reader = bufio.NewReaderSize(file, 64<<10)
			}
		}
		if file != nil {
			part, err := reader.ReadSlice('\n')
			offset += int64(len(part))
			if !oversized && pending.Len()+len(part) <= 64<<10 {
				pending.Write(part)
			} else {
				oversized = true
				pending.Reset()
			}
			switch err {
			case nil:
				if !oversized {
					if err := emit(strings.TrimSuffix(strings.TrimSuffix(pending.String(), "\n"), "\r")); err != nil {
						return err
					}
				}
				pending.Reset()
				oversized = false
				continue
			case bufio.ErrBufferFull:
				continue
			case io.EOF:
			default:
				return err
			}
			current, err := os.Stat(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err == nil {
				previous, err := file.Stat()
				if err != nil {
					return err
				}
				if !os.SameFile(current, previous) {
					file.Close()
					file = nil
					offset = 0
					pending.Reset()
					oversized = false
					continue
				}
				if current.Size() < offset {
					if _, err := file.Seek(0, io.SeekStart); err != nil {
						return err
					}
					reader.Reset(file)
					offset = 0
					pending.Reset()
					oversized = false
					continue
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wake:
		case <-ticker.C:
		}
	}
}

func notifications(ctx context.Context, dir string) <-chan struct{} {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC | syscall.IN_NONBLOCK)
	if err != nil {
		return nil
	}
	if _, err = syscall.InotifyAddWatch(fd, dir, syscall.IN_MODIFY|syscall.IN_CREATE|syscall.IN_MOVED_TO|syscall.IN_ATTRIB); err != nil {
		syscall.Close(fd)
		return nil
	}
	wake := make(chan struct{}, 1)
	go func() {
		defer syscall.Close(fd)
		tick := time.NewTicker(25 * time.Millisecond)
		defer tick.Stop()
		buf := make([]byte, 8192)
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				n, err := syscall.Read(fd, buf)
				if n > 0 {
					select {
					case wake <- struct{}{}:
					default:
					}
				}
				if err != nil && err != syscall.EAGAIN && err != syscall.EINTR {
					return
				}
			}
		}
	}()
	return wake
}
