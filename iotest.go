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

package iotest

import (
	"bytes"
	"crypto/sha1"
	"io"
	"math/rand"
	"testing"
)

type FileTester struct {
	F1, F2    io.ReadWriteSeeker
	T         *testing.T
	Rand      *rand.Rand
	MaxSize   int
	NumEvents int
}

func (ft *FileTester) Run() {
	if ft.Rand == nil {
		ft.Rand = rand.New(rand.NewSource(99))
	}
	if ft.MaxSize == 0 {
		ft.MaxSize = 1 << 20
	}
	if ft.NumEvents == 0 {
		ft.NumEvents = ft.Rand.Intn(2048)
	}
	for i := 0; i < ft.NumEvents; i++ {
		if ft.T.Failed() {
			break
		}
		switch ft.Rand.Intn(3) {
		case 0:
			ft.read()
		case 1:
			ft.write()
		case 2:
			ft.seek()
		}
	}

	if ft.T.Failed() {
		return
	}
	if _, err := ft.F1.Seek(0, 0); err != nil {
		ft.T.Fatal(err)
	}
	if _, err := ft.F2.Seek(0, 0); err != nil {
		ft.T.Fatal(err)
	}
	h := sha1.New()
	n1, err := io.Copy(h, ft.F1)
	if err != nil {
		ft.T.Fatal(err)
	}
	h1 := h.Sum(nil)

	h = sha1.New()
	n2, err := io.Copy(h, ft.F2)
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

func (ft *FileTester) read() {
	r1, r2 := ft.F1, ft.F2

	b1 := make([]byte, ft.Rand.Intn(ft.MaxSize))
	b2 := make([]byte, len(b1))

	steps := 0
	var n1 int
	var err1 error
	for n1 < len(b1) && err1 == nil {
		var nn int
		nn, err1 = r1.Read(b1[n1:])
		n1 += nn
		steps++
	}

	var n2 int
	var err2 error
	for n2 < len(b2) && err2 == nil {
		var nn int
		nn, err2 = r2.Read(b2[n2:])
		n2 += nn
	}

	ft.T.Logf("Read(make([]byte, %d)) n=%d, err=%v in %d steps", len(b1), n1, err1, steps)

	switch {
	case n1 != n2,
		(err1 == io.EOF && err2 != io.EOF) || (err1 != io.EOF && err2 == io.EOF),
		(err1 == nil && err2 != nil) || (err1 != nil && err2 == nil):
		ft.T.Errorf("Read(make([]byte, %d)) n=%d, err=%v, want n=%d, err=%v", len(b1), n1, err1, n2, err2)
	case !bytes.Equal(b1, b2):
		ft.T.Errorf("Read(make([]byte, %d)) bytes do not match", len(b1))
	}
}

func (ft *FileTester) write() {
	b := make([]byte, ft.Rand.Intn(ft.MaxSize))
	ft.Rand.Read(b)

	n1, err1 := ft.F1.Write(b)
	n2, err2 := ft.F2.Write(b)

	ft.T.Logf("Write(b) n=%d, err=%v", n1, err1)

	if n1 != n2 || (err1 == nil && err2 != nil) || (err1 != nil && err2 == nil) {
		ft.T.Errorf("Write(b), n=%d, err=%v, want n=%d, err=%v", n1, err1, n2, err2)
	}
}

func (ft *FileTester) seek() {
	offset := ft.Rand.Int63n(int64(ft.MaxSize))
	whence := ft.Rand.Intn(3)

	n1, err1 := ft.F1.Seek(offset, whence)
	n2, err2 := ft.F2.Seek(offset, whence)

	ft.T.Logf("Seek(%d, %d) n=%d, err=%v", offset, whence, n1, err1)

	if n1 != n2 || (err1 == nil && err2 != nil) || (err1 != nil && err2 == nil) {
		ft.T.Errorf("Seek(%d, %d), n=%d, err=%v, want n=%d, err=%v", offset, whence, n1, err1, n2, err2)
	}
}
