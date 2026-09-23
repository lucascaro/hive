package main

import (
	"bytes"
	"errors"
	"testing"
)

type failingWriter struct{ calls int }

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	return 0, errors.New("The handle is invalid.")
}

// A GUI-subsystem process on Windows has no usable stderr: every write
// to it fails. io.MultiWriter stops at the first failing writer, which
// silently lost the whole log file for a Hive started from the Start
// menu. The file is the record that matters; stderr is best-effort.
func TestLogTeeStillWritesTheFileWhenStderrFails(t *testing.T) {
	stderr := &failingWriter{}
	var file bytes.Buffer
	n, err := newLogTee(stderr, &file).Write([]byte("hello\n"))
	if err != nil || n != len("hello\n") {
		t.Fatalf("Write = (%d, %v), want (6, nil)", n, err)
	}
	if file.String() != "hello\n" {
		t.Fatalf("file got %q, want the record", file.String())
	}
	if stderr.calls != 1 {
		t.Fatalf("stderr written %d times, want the one best-effort attempt", stderr.calls)
	}
}

func TestLogTeeWritesBothWhenStderrWorks(t *testing.T) {
	var stderr, file bytes.Buffer
	if _, err := newLogTee(&stderr, &file).Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if stderr.String() != "x" || file.String() != "x" {
		t.Fatalf("stderr=%q file=%q, want both to hold the record", stderr.String(), file.String())
	}
}

func TestLogTeeReportsAFileFailure(t *testing.T) {
	var stderr bytes.Buffer
	if _, err := newLogTee(&stderr, &failingWriter{}).Write([]byte("x")); err == nil {
		t.Fatal("a failed file write must be reported, it is the one that matters")
	}
}
