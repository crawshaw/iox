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

package ioxtest // import "crawshaw.io/iox/ioxtest"

import (
	"bytes"
	"crypto/sha1"
	"io"
	"math/rand"
	"runtime/debug"
	"testing"
)

// TODO: rearrange operations into a generated table for minimization
// See github.com/dgryski/go-ddmin.

// Tester compares the I/O behavior of F1 and F2.
//
// The field F1 is tested, using F2 as a baseline.
// Tester checks F1 to see if it implements the following interfaces:
//
//	io.Reader
//	io.Writer
//	io.Seeker
//	io.ReaderAt
//	interface{ Truncate(size int64) error }
//
// Each interface that matches is added to a pool of potential
// operations, that are executed at random.
// All the operations are expected to match semantically on F1 and F2.
//
// If F1 implements io.Closer, then the object will be closed at
// the end and the resulting error compared to F2.
type Tester struct {
	F1, F2    interface{}
	T         *testing.T
	Rand      *rand.Rand
	MaxSize   int
	NumEvents int

	off, len int64
}

type truncater interface {
	Truncate(size int64) error
}

func (ft *Tester) Run() {
	if ft.Rand == nil {
		ft.Rand = rand.New(rand.NewSource(99))
	}
	if ft.MaxSize == 0 {
		ft.MaxSize = 1 << 20
	}
	if ft.NumEvents == 0 {
		ft.NumEvents = 2048
	}

	var tasks []func()
	if r, ok := ft.F1.(io.Reader); ok {
		tasks = append(tasks, func() {
			ft.read(r, ft.F2.(io.Reader))
		})
	}
	if w, ok := ft.F1.(io.Writer); ok {
		tasks = append(tasks, func() {
			ft.write(w, ft.F2.(io.Writer))
		})
	}
	if s, ok := ft.F1.(io.Seeker); ok {
		tasks = append(tasks, func() {
			ft.seek(s, ft.F2.(io.Seeker))
		})
	}
	if s, ok := ft.F1.(io.ReaderAt); ok {
		tasks = append(tasks, func() {
			ft.readAt(s, ft.F2.(io.ReaderAt))
		})
	}
	if s, ok := ft.F1.(truncater); ok {
		tasks = append(tasks, func() {
			ft.truncate(s, ft.F2.(truncater))
		})
	}

	for i := 0; i < ft.NumEvents; i++ {
		if ft.T.Failed() {
			break
		}
		fn := tasks[ft.Rand.Intn(len(tasks))]
		func() {
			defer func() {
				if r := recover(); r != nil {
					ft.T.Errorf("task paniced (off=%d, len=%d): %s", ft.off, ft.len, debug.Stack())
				}
			}()
			fn()
		}()
	}

	if !ft.T.Failed() {
		ft.finalCompare()
	}

	if c1, ok := ft.F1.(io.Closer); ok {
		if c2, ok := ft.F2.(io.Closer); ok {
			err1 := c1.Close()
			err2 := c2.Close()

			if (err1 == nil && err2 != nil) || (err1 != nil && err2 == nil) {
				ft.T.Errorf("Close err=%v, want %v", err1, err2)
			}
		}
	}
}

func (ft *Tester) finalCompare() {
	if s1, ok := ft.F1.(io.Seeker); ok {
		s2 := ft.F2.(io.Seeker)
		if _, err := s1.Seek(0, 0); err != nil {
			ft.T.Fatal(err)
		}
		if _, err := s2.Seek(0, 0); err != nil {
			ft.T.Fatal(err)
		}
	}
	if r1, ok := ft.F1.(io.Reader); ok {
		r2 := ft.F2.(io.Reader)
		h := sha1.New()
		n1, err := io.Copy(h, r1)
		if err != nil {
			ft.T.Fatal(err)
		}
		h1 := h.Sum(nil)

		h = sha1.New()
		n2, err := io.Copy(h, r2)
		if err != nil {
			ft.T.Fatal(err)
		}
		h2 := h.Sum(nil)
		if n1 != n2 {
			ft.T.Fatalf("final file is %d bytes, want %d bytes", n1, n2)
		}
		if !bytes.Equal(h1, h2) {
			ft.T.Fatalf("final file has wrong hash %x, want %x", h1, h2)
		}

	}
}

func (ft *Tester) read(r1, r2 io.Reader) {
	b1 := make([]byte, ft.Rand.Intn(ft.MaxSize))
	b2 := make([]byte, len(b1))

	var steps int
	var n1 int
	var err1 error
	defer func() {
		ft.T.Logf("Read(make([]byte, %d)) n=%d, err=%v in %d steps", len(b1), n1, err1, steps)
	}()

	for n1 < len(b1) && err1 == nil {
		var nn int
		nn, err1 = r1.Read(b1[n1:])
		n1 += nn
		steps++
	}

	ft.off += int64(n1)

	var n2 int
	var err2 error
	for n2 < len(b2) && err2 == nil {
		var nn int
		nn, err2 = r2.Read(b2[n2:])
		n2 += nn
	}

	switch {
	case n1 != n2,
		(err1 == io.EOF && err2 != io.EOF) || (err1 != io.EOF && err2 == io.EOF),
		(err1 == nil && err2 != nil) || (err1 != nil && err2 == nil):
		ft.T.Errorf("Read(b1[:%d]) n=%d, err=%v, want n=%d, err=%v", len(b1), n1, err1, n2, err2)
	case !bytes.Equal(b1, b2):
		ft.T.Errorf("Read(b1[:%d]) bytes do not match", len(b1))
	}
}

func (ft *Tester) readAt(r1, r2 io.ReaderAt) {
	b1 := make([]byte, ft.Rand.Intn(ft.MaxSize))
	b2 := make([]byte, len(b1))
	off := int64(ft.Rand.Intn(ft.MaxSize))

	var n1 int
	var err1 error
	defer func() {
		ft.T.Logf("ReadAt(b1[:%d]), %d) n=%d, err=%v", len(b1), off, n1, err1)
	}()

	n1, err1 = r1.ReadAt(b1, off)
	n2, err2 := r2.ReadAt(b2, off)

	switch {
	case n1 != n2,
		(err1 == io.EOF && err2 != io.EOF) || (err1 != io.EOF && err2 == io.EOF),
		(err1 == nil && err2 != nil) || (err1 != nil && err2 == nil):
		ft.T.Errorf("ReadAt(b1[:%d], %d) n=%d, err=%v, want n=%d, err=%v", len(b1), off, n1, err1, n2, err2)
	case !bytes.Equal(b1, b2):
		ft.T.Errorf("ReadAt(b1[:%d], %d) bytes do not match", len(b1), off)
	}
}

func (ft *Tester) write(w1, w2 io.Writer) {
	b := make([]byte, ft.Rand.Intn(ft.MaxSize))
	ft.Rand.Read(b)

	var n1 int
	var err1 error
	defer func() {
		ft.T.Logf("Write(b) len(b)=%d, n=%d, err=%v", len(b), n1, err1)
	}()

	n1, err1 = w1.Write(b)
	ft.off += int64(n1)
	if ft.off > ft.len {
		ft.len = ft.off
	}

	n2, err2 := w2.Write(b)

	if n1 != n2 || (err1 == nil && err2 != nil) || (err1 != nil && err2 == nil) {
		ft.T.Errorf("Write(b), n=%d, err=%v, want n=%d, err=%v", n1, err1, n2, err2)
	}
}

func (ft *Tester) seek(s1, s2 io.Seeker) {
	// TODO: negative offset values
	offset := ft.Rand.Int63n(int64(ft.MaxSize))
	whence := ft.Rand.Intn(3)

	var n1 int64
	var err1 error
	defer func() {
		ft.T.Logf("Seek(%d, %d) n=%d, err=%v", offset, whence, n1, err1)
	}()

	n1, err1 = s1.Seek(offset, whence)
	n2, err2 := s2.Seek(offset, whence)

	if n1 != n2 || (err1 == nil && err2 != nil) || (err1 != nil && err2 == nil) {
		ft.T.Errorf("Seek(%d, %d), n=%d, err=%v, want n=%d, err=%v", offset, whence, n1, err1, n2, err2)
	}

	// From the io.Seeker docs:
	//
	//	Seeking to any positive offset is legal, but the
	// 	behavior of subsequent I/O operations on the
	// 	underlying object is implementation-dependent.
	//
	// To avoid testing implementation-dependent features, if
	// our seek went beyond the end of the file, rewind to the
	// end.
	if n1 > ft.len {
		if _, err := s1.Seek(ft.len, 0); err != nil {
			ft.T.Errorf("Seek(%d, 0): rewind failed: %v", ft.len, err)
		}
		if _, err := s2.Seek(ft.len, 0); err != nil {
			ft.T.Errorf("Seek(%d, 0): rewind of base object failed: %v", ft.len, err)
		}
		ft.off = ft.len
	}
}

func (ft *Tester) truncate(s1, s2 truncater) {
	size := ft.Rand.Int63n(int64(ft.MaxSize))

	var err1 error
	defer func() {
		ft.T.Logf("Truncate(%d) err=%v", size, err1)
	}()

	err1 = s1.Truncate(size)
	err2 := s2.Truncate(size)

	if (err1 == nil && err2 != nil) || (err1 != nil && err2 == nil) {
		ft.T.Errorf("Truncate(%d), err=%v, want err=%v", size, err1, err2)
	}
}
