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
	"os"
	"strings"
	"testing"
)

func TestFiler(t *testing.T) {
	filer := NewFiler(0)
	f1, err := filer.TempFile("", "testfile1", ".txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f1.Name(), "testfile1") {
		t.Errorf("temp file %q does not include 'testfile1' prefix", f1.Name())
	}
	if !strings.HasSuffix(f1.Name(), ".txt") {
		t.Errorf("temp file %q does not have '.txt' suffix", f1.Name())
	}

	f1dup, err := filer.Open(f1.Name())
	if err != nil {
		t.Fatal(err)
	}
	f1name := f1.Name()
	if f1dup.Name() != f1name {
		t.Errorf("f1dup.Name()=%q, want %q", f1dup.Name(), f1name)
	}
	if err := f1dup.Close(); err != nil {
		t.Fatal(err)
	}

	f2, err := filer.TempFile("", "testfile1", ".txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := f2.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f1.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f1.Close(); err != os.ErrClosed {
		t.Errorf("second close of f1 err=%v, want os.ErrClosed", err)
	}

	if _, err := os.Stat(f1name); err == nil {
		t.Errorf("could stat temp file %q after close", f1name)
	}
}
