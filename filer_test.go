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
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
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

	if _, err := filer.Open("/doesnotexist"); !os.IsNotExist(err) {
		t.Errorf(`Open("/doesnotexist") err=%v, want os.IsNotExist`, err)
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

	f1dup, err = filer.OpenFile(f1.Name(), os.O_RDONLY, 0600)
	if err != nil {
		t.Fatal(err)
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
	err = f1.Close()
	if underlyingError(err) != os.ErrClosed {
		t.Errorf("second close of f1 err=%v (%T), want os.ErrClosed", err, err)
	}

	if _, err := os.Stat(f1name); err == nil {
		t.Errorf("could stat temp file %q after close", f1name)
	}
}

func underlyingError(err error) error {
	if err == nil {
		return err
	}
	if perr, _ := err.(*os.PathError); perr != nil {
		return perr.Err
	}
	return err
}

func TestFilerShutdownClean(t *testing.T) {
	filer := NewFiler(2)
	f1, err := filer.TempFile("", "testfile1", "")
	if err != nil {
		t.Fatal(err)
	}
	f2, err := filer.TempFile("", "testfile2", "")
	if err != nil {
		t.Fatal(err)
	}

	f3ch := make(chan error)
	go func() {
		f3, err := filer.TempFile("", "testfile3", "")
		if f3 != nil {
			f3.Close()
		}

		// At this point, Shutdown has been triggered.
		f1.Close()
		f2.Close()

		f3ch <- err
	}()

	time.Sleep(10 * time.Millisecond)
	filer.Shutdown(context.Background())

	f3err := <-f3ch
	if f3err != context.Canceled {
		t.Errorf("f3 create error: %v, want context Canceled", f3err)
	}

	if _, err := filer.OpenFile(filepath.Join(os.TempDir(), "never-created"), os.O_CREATE, 0600); err != context.Canceled {
		t.Errorf("shutdown-then-OpenFile err=%v, want context.Canceled", err)
	}
	if _, err := filer.Open(filepath.Join(os.TempDir(), "never-created")); err != context.Canceled {
		t.Errorf("shutdown-then-Open err=%v, want context.Canceled", err)
	}
}

func openAndCloseTempFile(filer *Filer) error {
	f, err := filer.TempFile("", "temp-file-opened-and-closed", "")
	if f != nil {
		err = f.Close()
	}
	return err
}
func openATempFile(filer *Filer) (*File, error) { return filer.TempFile("", "a-temp-file", "") }
func openBufferFile1(filer *Filer) *BufferFile  { return filer.BufferFile(1) }
func openBufferFile2(filer *Filer) *BufferFile  { return filer.BufferFile(1) }

func TestFilerShutdownForced(t *testing.T) {
	buf := new(bytes.Buffer)
	bufLogf := func(format string, v ...interface{}) {
		fmt.Fprintf(buf, format, v...)
		buf.WriteByte('\n')
	}

	filer := NewFiler(3) // f1, f2, bf2
	filer.Logf = bufLogf
	f1, err := filer.TempFile("", "testfile1", "")
	if err != nil {
		t.Fatal(err)
	}
	f2, err := openATempFile(filer)
	if err != nil {
		t.Fatal(err)
	}

	if err := openAndCloseTempFile(filer); err != nil {
		t.Fatal(err)
	}

	bf1 := openBufferFile1(filer)
	defer bf1.Close()

	bf2 := openBufferFile2(filer)
	if _, err := bf2.Write([]byte{'a'}); err != nil {
		t.Fatal(err)
	}
	if _, err := bf2.Write([]byte{'b'}); err != nil { // bf2 file created here
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond)

	errCh := make(chan error)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		errCh <- filer.Shutdown(ctx)
	}()

	time.Sleep(10 * time.Millisecond)
	if _, err := filer.TempFile("", "canceled file", ""); err != context.Canceled {
		t.Errorf("TempFile opened after Shutdown reports err %v, want context.Canceled", err)
	}

	cancel()
	if err := <-errCh; err != context.Canceled {
		t.Errorf("filer.Shutdown(ctx)=%v, want context.Canceled", err)
	}

	_ = f2
	if err := underlyingError(f1.Close()); err != os.ErrClosed {
		t.Errorf("f1.Close()=%v, want os.ErrClosed", err)
	}

	log := buf.String()
	if strings.Contains(log, "openAndCloseTempFile") {
		t.Error("log mentions file from openAndCloseTempFile that should already be closed")
	}
	if strings.Contains(log, "openBufferFile1") {
		t.Error("log mentions BufferFile1, which should never grab resources")
	}
	if !strings.Contains(log, "openBufferFile2") {
		t.Error("log does not mention BufferFile2, which hold resources")
	}
	if !regexp.MustCompile(`waiting for [^\n]*iox.TestFilerShutdownForced`).MatchString(log) {
		t.Error("log does not metion waiting for file from TestFilerShutdownForced")
	}
	if !regexp.MustCompile(`waiting for [^\n]*iox.openATempFile`).MatchString(log) {
		t.Error("log does not metion waiting for file from openATempFile")
	}
	if !regexp.MustCompile(`closing file [^\n]*iox.TestFilerShutdownForced`).MatchString(log) {
		t.Errorf("log does not metion closing file from TestFilerShutdownForced")
	}
	if !regexp.MustCompile(`closing file [^\n]*iox.openATempFile`).MatchString(log) {
		t.Errorf("log does not metion closing file from openATempFile")
	}
	if t.Failed() {
		t.Logf("filer log:\n%s", log)
	}
}

func TestFileNilClose(t *testing.T) {
	var f *File
	if err := f.Close(); err != os.ErrInvalid {
		t.Errorf("f.Close()=%v, want os.ErrInvalid", err)
	}
}
