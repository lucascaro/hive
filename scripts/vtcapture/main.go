// Command vtcapture records a real PTY byte stream into a conformance fixture
// for internal/session/testdata/conformance/.
//
// Usage:
//
//	go run ./scripts/vtcapture -out internal/session/testdata/conformance/vim-hello \
//	    -cols 80 -rows 24 -- vim -c "echo 'hello'" -c "sleep 1" -c "qa"
//
// Writes three files into -out:
//
//   - input.bin        every byte received from the PTY master, in order
//   - meta.json        {"cols", "rows", "description", "chunks": [...]}
//   - golden.snapshot  current backend's RenderSnapshot output for this stream
//
// chunks records the byte length of each PTY read, preserving the chunk
// boundaries the conformance test will replay through VT.Write. Real PTY
// streams' chunking matters: the snapshot path's eviction heuristic and the
// (future) UTF-8 split-decode path both depend on it.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/creack/pty"

	"github.com/lucascaro/hive/internal/session"
)

type captureMeta struct {
	Cols        int    `json:"cols"`
	Rows        int    `json:"rows"`
	Description string `json:"description,omitempty"`
	Chunks      []int  `json:"chunks,omitempty"`
}

func main() {
	cols := flag.Int("cols", 80, "PTY column count")
	rows := flag.Int("rows", 24, "PTY row count")
	out := flag.String("out", "", "output directory (will be created)")
	desc := flag.String("desc", "", "human description for meta.json (defaults to the command line)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s -out DIR [-cols N] [-rows N] [-desc TEXT] -- cmd [args...]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	args := flag.Args()
	if *out == "" || len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if *cols <= 0 || *rows <= 0 {
		fatalf("cols/rows must be positive; got cols=%d rows=%d", *cols, *rows)
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatalf("mkdir %s: %v", *out, err)
	}

	cmd := exec.Command(args[0], args[1:]...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(*cols), Rows: uint16(*rows)})
	if err != nil {
		fatalf("start pty: %v", err)
	}
	defer func() { _ = ptmx.Close() }()

	var (
		buf    []byte
		chunks []int
		read   = make([]byte, 4096)
	)
	for {
		n, err := ptmx.Read(read)
		if n > 0 {
			buf = append(buf, read[:n]...)
			chunks = append(chunks, n)
		}
		if err != nil {
			if err != io.EOF {
				// EIO is the normal "PTY child exited" signal on Linux.
				// Don't treat it as fatal; we already captured everything.
				if !isPTYHangup(err) {
					fmt.Fprintf(os.Stderr, "warning: pty read: %v\n", err)
				}
			}
			break
		}
	}
	_ = cmd.Wait()

	description := *desc
	if description == "" {
		description = strings.Join(args, " ")
	}
	meta := captureMeta{Cols: *cols, Rows: *rows, Description: description, Chunks: chunks}

	if err := os.WriteFile(filepath.Join(*out, "input.bin"), buf, 0o644); err != nil {
		fatalf("write input.bin: %v", err)
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		fatalf("marshal meta: %v", err)
	}
	metaBytes = append(metaBytes, '\n')
	if err := os.WriteFile(filepath.Join(*out, "meta.json"), metaBytes, 0o644); err != nil {
		fatalf("write meta.json: %v", err)
	}

	// Generate golden by replaying through the current backend exactly the
	// way the conformance test will, including chunk boundaries.
	v := session.NewVT(*cols, *rows)
	off := 0
	for _, n := range chunks {
		if _, err := v.Write(buf[off : off+n]); err != nil {
			fatalf("VT.Write replay chunk: %v", err)
		}
		off += n
	}
	golden := v.RenderSnapshot()
	if err := os.WriteFile(filepath.Join(*out, "golden.snapshot"), golden, 0o644); err != nil {
		fatalf("write golden.snapshot: %v", err)
	}

	fmt.Printf("wrote %s: %d bytes input across %d chunks, %d bytes golden\n",
		*out, len(buf), len(chunks), len(golden))
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// isPTYHangup reports whether err is the "PTY child exited" signal that
// surfaces as EIO/closed on Read after the slave end is closed. We treat
// these as a clean end-of-capture, not a fatal read failure.
func isPTYHangup(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "input/output error") ||
		strings.Contains(s, "file already closed")
}
