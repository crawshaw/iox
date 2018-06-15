// Copyright (c) 2018 David Crawshaw <david@zentus.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package iox

import (
	"fmt"
	"io"
	"os"
)

// BufferFile creates a buffered file with up to memSize bytes stored in memory.
//
// The underlying file descriptor should be carefully avoided as the
// fraction of the contents stored in the OS file may change.
func (f *Filer) BufferFile(memSize int) *BufferFile {
	const defaultMemSize = 1 << 16
	if memSize == 0 {
		memSize = defaultMemSize
	}
	return &BufferFile{
		filer:  f,
		bufMax: memSize,
	}
}

// BufferFile is a temporary file with a a fixed prefix stored in memory.
//
// This allows for typical cases where the contents fit in the prefix to
// avoid the disk entirely and not hold a file descriptor.
type BufferFile struct {
	io.Reader
	// TODO: io.ReaderAt
	io.Writer
	io.Seeker
	io.Closer

	err    error
	filer  *Filer
	bufMax int
	buf    []byte
	f      *File
	flen   int64 // current length of f

	off int64 // kept in sync with pos in *File
}

func (bf *BufferFile) ensureFile() error {
	if bf.f == nil {
		bf.f, bf.err = bf.filer.TempFile("", "bufferfile-", "")
	}
	return bf.err
}

func (bf *BufferFile) Write(p []byte) (n int, err error) {
	if bf.err != nil {
		return 0, bf.err
	}
	finalOff := bf.off + int64(len(p))
	if finalOff > int64(len(bf.buf)) && len(bf.buf) < bf.bufMax {
		growTo := int(n)
		if growTo > bf.bufMax {
			growTo = bf.bufMax
		}
		for len(bf.buf) < growTo {
			bf.buf = append(bf.buf, 0)
		}
	}
	if finalOff >= int64(bf.bufMax) {
		bf.ensureFile()
	}
	if bf.off < int64(len(bf.buf)) {
		n = len(bf.buf) - int(bf.off)
		if n > len(p) {
			n = len(p)
		}
		copy(bf.buf[bf.off:], p[:n])
		bf.off += int64(n)
		p = p[n:]
	}
	if len(p) == 0 {
		return n, nil // done, the write fit in the memory buffer
	}
	n2, err := bf.f.Write(p)
	bf.err = err
	n += n2
	bf.off += int64(n2)
	if fpos := bf.off - int64(len(bf.buf)); fpos > bf.flen {
		bf.flen = fpos
	}
	return n, err
}

func (bf *BufferFile) Read(p []byte) (n int, err error) {
	if bf.err != nil {
		return 0, bf.err
	}
	if bf.off < int64(len(bf.buf)) {
		n = copy(p, bf.buf[bf.off:])
		bf.off += int64(n)
		return n, nil
	}
	if bf.f == nil {
		return 0, io.EOF
	}
	n, err = bf.f.Read(p)
	bf.off += int64(n)
	if err != io.EOF {
		bf.err = err
	}
	return n, err
}

func (bf *BufferFile) Seek(offset int64, whence int) (int64, error) {
	if bf.err != nil {
		return 0, bf.err
	}

	const (
		SeekStart   = 0
		SeekCurrent = 1
		SeekEnd     = 2
	)
	switch whence {
	case os.SEEK_SET:
		// use offset directly
	case os.SEEK_CUR:
		offset += bf.off
	case os.SEEK_END:
		offset += int64(len(bf.buf)) + bf.flen
	}
	if offset < 0 {
		return -1, fmt.Errorf("iox.BufferFile: attempting to seek before beginning of BufferFile (%d)", offset)
	}
	if offset > int64(bf.bufMax) {
		bf.ensureFile()
	}
	if offset < int64(len(bf.buf)) {
		if bf.f != nil {
			_, bf.err = bf.f.Seek(0, os.SEEK_SET)
		}
	} else {
		_, bf.err = bf.f.Seek(offset-int64(len(bf.buf)), os.SEEK_SET)
	}
	bf.off = offset

	return offset, bf.err
}

// TODO: func (bf *BufferFile) Truncate

// Close closes the BufferFile, deleting any underlying temporary file.
func (bf *BufferFile) Close() (err error) {
	if bf.f != nil {
		err = bf.f.Close()
		bf.f = nil
	}
	if err != nil {
		bf.err = err
		return err
	}
	if bf.err == nil {
		bf.err = os.ErrClosed
	}
	return nil
}
