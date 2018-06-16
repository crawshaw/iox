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
	"math/rand"
	"testing"

	"crawshaw.io/iox/ioxtest"
)

func invariants(t *testing.T, bf *BufferFile) {
	// TODO: test these regularly

	// Some invariants and state details:
	// len(buf) <= bufMax always
	// len(buf) < bufMax  => the entire contents is in memory.
	// len(buf) < bufMax  => f == nil || f.Seek(0, 1) == 0
	// f == nil || f.Seek(0, 1) == flen
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
		F1:   bf,
		F2:   f,
		T:    t,
		Rand: testRand,
	}
	ft.Run()

	if err := bf.Close(); err != nil {
		t.Error(err)
	}
}
