package memfs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"sync"
	"time"

	"github.com/go-git/go-billy/v6"
)

type file struct {
	name     string
	content  *content
	position int64
	flag     int
	mode     os.FileMode
	modTime  time.Time

	isClosed bool
}

func (f *file) Name() string {
	return f.name
}

func (f *file) Read(b []byte) (int, error) {
	n, err := f.ReadAt(b, f.position)
	f.position += int64(n)

	if errors.Is(err, io.EOF) && n != 0 {
		err = nil
	}

	return n, err
}

func (f *file) ReadAt(b []byte, off int64) (int, error) {
	if f.isClosed {
		return 0, os.ErrClosed
	}

	if !isReadAndWrite(f.flag) && !isReadOnly(f.flag) {
		return 0, errors.New("read not supported")
	}

	n, err := f.content.ReadAt(b, off)

	return n, err
}

func (f *file) Seek(offset int64, whence int) (int64, error) {
	if f.isClosed {
		return 0, os.ErrClosed
	}

	switch whence {
	case io.SeekCurrent:
		f.position += offset
	case io.SeekStart:
		f.position = offset
	case io.SeekEnd:
		f.position = int64(f.content.Len()) + offset
	}

	return f.position, nil
}

func (f *file) Write(p []byte) (int, error) {
	return f.WriteAt(p, f.position)
}

func (f *file) WriteAt(p []byte, off int64) (int, error) {
	if f.isClosed {
		return 0, os.ErrClosed
	}

	if !isReadAndWrite(f.flag) && !isWriteOnly(f.flag) {
		return 0, errors.New("write not supported")
	}

	f.modTime = time.Now()
	n, err := f.content.WriteAt(p, off)
	f.position = off + int64(n)

	return n, err
}

func (f *file) Close() error {
	if f.isClosed {
		return os.ErrClosed
	}

	f.isClosed = true
	return nil
}

func (f *file) Truncate(size int64) error {
	if size < int64(len(f.content.bytes)) {
		f.content.bytes = f.content.bytes[:size]
	} else if more := int(size) - len(f.content.bytes); more > 0 {
		f.content.bytes = append(f.content.bytes, make([]byte, more)...)
	}

	return nil
}

func (f *file) Duplicate(filename string, mode fs.FileMode, flag int) billy.File {
	n := &file{
		name:    filename,
		content: f.content,
		mode:    mode,
		flag:    flag,
		modTime: f.modTime,
	}

	if isTruncate(flag) {
		n.content.Truncate()
	}

	if isAppend(flag) {
		n.position = int64(n.content.Len())
	}

	return n
}

func (f *file) Stat() (os.FileInfo, error) {
	return &fileInfo{
		name:    f.Name(),
		mode:    f.mode,
		size:    f.content.Len(),
		modTime: f.modTime,
	}, nil
}

// Lock is a no-op in memfs.
func (f *file) Lock() error {
	return nil
}

// Unlock is a no-op in memfs.
func (f *file) Unlock() error {
	return nil
}

type fileInfo struct {
	name    string
	size    int
	mode    os.FileMode
	modTime time.Time
}

func (fi *fileInfo) Name() string {
	return fi.name
}

func (fi *fileInfo) Size() int64 {
	return int64(fi.size)
}

func (fi *fileInfo) Mode() fs.FileMode {
	return fi.mode
}

func (fi *fileInfo) ModTime() time.Time {
	return fi.modTime
}

func (fi *fileInfo) IsDir() bool {
	return fi.mode.IsDir()
}

func (*fileInfo) Sys() interface{} {
	return nil
}

type content struct {
	name  string
	bytes []byte

	m sync.RWMutex
}

func (c *content) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, &os.PathError{
			Op:   "writeat",
			Path: c.name,
			Err:  errors.New("negative offset"),
		}
	}

	c.m.Lock()
	prev := len(c.bytes)

	diff := int(off) - prev
	if diff > 0 {
		c.bytes = append(c.bytes, make([]byte, diff)...)
	}

	c.bytes = append(c.bytes[:off], p...)
	if len(c.bytes) < prev {
		c.bytes = c.bytes[:prev]
	}
	c.m.Unlock()

	return len(p), nil
}

func (c *content) ReadAt(b []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, &os.PathError{
			Op:   "readat",
			Path: c.name,
			Err:  errors.New("negative offset"),
		}
	}

	c.m.RLock()
	size := int64(len(c.bytes))
	if off >= size {
		c.m.RUnlock()
		return 0, io.EOF
	}

	l := int64(len(b))
	if off+l > size {
		l = size - off
	}

	btr := c.bytes[off : off+l]
	n = copy(b, btr)

	if len(btr) < len(b) {
		err = io.EOF
	}
	c.m.RUnlock()

	return
}
