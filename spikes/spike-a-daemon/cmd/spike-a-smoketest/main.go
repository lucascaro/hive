// spike-a-smoketest exercises hived-spike without needing a TTY:
// it speaks the framed protocol directly. Used by CI/dev to verify
// the spike's acceptance criteria around detach/reattach + persistence.
//
// Throwaway. Will be deleted with the rest of Spike A.
package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/lucascaro/hive/spikes/spike-a-daemon/internal/proto"
)

func dial() (net.Conn, error) {
	return net.Dial("unix", proto.SocketPath(os.Getuid()))
}

// drain reads frames into a buffer for d.
func drain(c net.Conn, d time.Duration) []byte {
	deadline := time.Now().Add(d)
	_ = c.SetReadDeadline(deadline)
	var out bytes.Buffer
	for {
		t, data, err := proto.ReadFrame(c)
		if err != nil {
			return out.Bytes()
		}
		if t == proto.FrameData {
			out.Write(data)
		}
	}
}

func send(c net.Conn, s string) error {
	return proto.WriteFrame(c, proto.FrameData, []byte(s))
}

func main() {
	fail := func(msg string, args ...any) {
		fmt.Fprintf(os.Stderr, "FAIL: "+msg+"\n", args...)
		os.Exit(1)
	}

	// Step 1: attach, set state.
	c1, err := dial()
	if err != nil {
		fail("dial 1: %v", err)
	}
	// drain initial prompt + replay buffer
	_ = drain(c1, 500*time.Millisecond)

	if err := send(c1, "cd /tmp && export FOO=bar_spike_marker\n"); err != nil {
		fail("send 1: %v", err)
	}
	out1 := drain(c1, 500*time.Millisecond)
	_ = c1.Close()
	fmt.Printf("[after first session] saw %d bytes\n", len(out1))

	// Step 2: reattach, verify state persisted.
	time.Sleep(200 * time.Millisecond)
	c2, err := dial()
	if err != nil {
		fail("dial 2: %v", err)
	}
	// drain replay
	replay := drain(c2, 500*time.Millisecond)
	fmt.Printf("[reattach replay] %d bytes\n", len(replay))

	probe := "spike_probe_" + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := send(c2, "echo "+probe+":$FOO:$(pwd)\n"); err != nil {
		fail("send 2: %v", err)
	}
	out2 := drain(c2, 2500*time.Millisecond)
	_ = c2.Close()

	want := probe + ":bar_spike_marker:/tmp"
	if !strings.Contains(string(out2), want) {
		fmt.Fprintf(os.Stderr, "[reattach session output, %d bytes]\n%s\n---\n", len(out2), string(out2))
		fail("expected substring %q not found in reattach output", want)
	}

	fmt.Println("OK: state survived detach/reattach")
	fmt.Println("    - $FOO=bar_spike_marker preserved")
	fmt.Println("    - cwd=/tmp preserved")

	// Step 3: resize check — set a non-default size and verify the shell sees it.
	c3, err := dial()
	if err != nil {
		fail("dial 3: %v", err)
	}
	_ = drain(c3, 300*time.Millisecond)
	if err := proto.WriteFrame(c3, proto.FrameResize, proto.EncodeResize(137, 41)); err != nil {
		fail("send resize: %v", err)
	}
	// Give the kernel a moment to deliver SIGWINCH.
	time.Sleep(100 * time.Millisecond)
	if err := send(c3, "stty size\n"); err != nil {
		fail("send stty: %v", err)
	}
	out3 := drain(c3, 1500*time.Millisecond)
	_ = c3.Close()

	if !strings.Contains(string(out3), "41 137") {
		fmt.Fprintf(os.Stderr, "[resize session output]\n%s\n---\n", string(out3))
		fail("expected shell to report 41 rows × 137 cols")
	}
	fmt.Println("OK: resize propagated to shell (41×137)")
}
