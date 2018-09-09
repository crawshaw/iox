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
	"io"
	"math/rand"
	"os"
	"testing"

	"crawshaw.io/iox/ioxtest"
)

func invariants(t *testing.T, bf *BufferFile) {
	if len(bf.buf) > bf.bufMax {
		t.Fatalf("len(bf.buf)=%d > bf.bufMax=%d", len(bf.buf), bf.bufMax)
	}
	if len(bf.buf) < bf.bufMax {
		if bf.flen != 0 {
			t.Fatalf("len(bf.buf)=%d < bf.bufMax=%d but bf.flen=%d", len(bf.buf), bf.bufMax, bf.flen)
		}
	}
	if bf.f != nil {
		foff, err := bf.f.Seek(0, os.SEEK_CUR)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := bf.f.Seek(foff, os.SEEK_SET); err != nil {
				t.Fatal(err)
			}
		}()
		if foffWant := bf.off - int64(bf.bufMax); foffWant < 0 {
			if foff != 0 {
				t.Fatalf("bf.off=%d < bf.bufMax=%d but bf.f.Seek(0, 1)=%d, not 0", bf.off, bf.bufMax, foff)
			}
		} else {
			if foff != foffWant {
				t.Fatalf("(bf.off=%d - bf.bufMax=%d)=%d != bf.f.Seek(0, 1)=%d", bf.off, bf.bufMax, foffWant, foff)
			}
		}

		flen, err := bf.f.Seek(0, os.SEEK_END)
		if err != nil {
			t.Fatal(err)
		}
		if bf.flen != flen {
			t.Fatalf("bf.flen=%d != bf.f.Seek(0, 2)=%d", bf.flen, flen)
		}
	}
}

func TestBufferFileDefault(t *testing.T) {
	filer := NewFiler(1)
	bf := filer.BufferFile(0)
	if _, err := bf.Read(make([]byte, 3)); err != io.EOF {
		t.Errorf("empty Read err=%v, want io.EOF", err)
	}
	if _, err := bf.ReadAt(make([]byte, 3), 0); err != io.EOF {
		t.Errorf("empty ReadAt err=%v, want io.EOF", err)
	}
	if _, err := bf.Seek(-1, os.SEEK_SET); err == nil {
		t.Error("negative Seek returned no error")
	}
	if _, err := bf.Write([]byte("hello")); err != nil {
		t.Error(err)
	}
	if n := bf.Len(); n != 5 {
		t.Errorf("Len()=%d, want 5", n)
	}
	if bf.f != nil {
		t.Error("default buffer size should not need a file for a small write")
	}
	bf.Close()
}

func TestBufferFileSmall(t *testing.T) {
	filer := NewFiler(2)

	bf := filer.BufferFile(4096)
	f, err := filer.TempFile("", "cmpfile-", "")
	if err != nil {
		t.Fatal(err)
	}

	ft := &ioxtest.Tester{
		F1:      bf,
		F2:      f,
		T:       t,
		MaxSize: 4096,
	}
	ft.Run()

	if bf.f != nil {
		t.Error("small file events caused BufferFile to create a backing file")
	}
	if err := bf.Close(); err != nil {
		t.Error(err)
	}
}

// testRand is shared across runs to make -count=N more interesting.
var testRand = rand.New(rand.NewSource(107))

func TestBufferFile(t *testing.T) {
	filer := NewFiler(2)

	bf := filer.BufferFile(1024)
	f, err := filer.TempFile("", "cmpfile-", "")
	if err != nil {
		t.Fatal(err)
	}

	ft := &ioxtest.Tester{
		F1:         bf,
		F2:         f,
		T:          t,
		Rand:       testRand,
		Invariants: func() { invariants(t, bf) },
	}
	ft.Run()

	if err := bf.Close(); err != nil {
		t.Error(err)
	}
}
