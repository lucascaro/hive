package muxnative

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

func TestWriteReadMsg_Roundtrip(t *testing.T) {
	req := Request{Op: "capture", Target: "hive-sessions:3", Lines: 50}
	var buf bytes.Buffer
	if err := writeMsg(&buf, req); err != nil {
		t.Fatalf("writeMsg: %v", err)
	}
	var got Request
	if err := readMsg(&buf, &got); err != nil {
		t.Fatalf("readMsg: %v", err)
	}
	if got.Op != "capture" || got.Target != "hive-sessions:3" || got.Lines != 50 {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
}

func TestWriteReadMsg_Response(t *testing.T) {
	resp := Response{OK: true, Content: "hello world", Int: 42}
	var buf bytes.Buffer
	if err := writeMsg(&buf, resp); err != nil {
		t.Fatalf("writeMsg: %v", err)
	}
	var got Response
	if err := readMsg(&buf, &got); err != nil {
		t.Fatalf("readMsg: %v", err)
	}
	if !got.OK || got.Content != "hello world" || got.Int != 42 {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
}

func TestWriteMsg_HeaderFormat(t *testing.T) {
	var buf bytes.Buffer
	if err := writeMsg(&buf, "test"); err != nil {
		t.Fatalf("writeMsg: %v", err)
	}
	data := buf.Bytes()
	if len(data) < 4 {
		t.Fatalf("output too short: %d bytes", len(data))
	}
	msgLen := binary.BigEndian.Uint32(data[:4])
	if int(msgLen) != len(data)-4 {
		t.Errorf("header says %d bytes, body is %d bytes", msgLen, len(data)-4)
	}
}

func TestReadMsg_TooLarge(t *testing.T) {
	var buf bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], maxMsgSize+1)
	buf.Write(hdr[:])
	var got Response
	err := readMsg(&buf, &got)
	if err == nil {
		t.Fatal("readMsg should reject oversized message")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %q, want 'too large'", err)
	}
}

func TestReadMsg_ExactlyMaxSize(t *testing.T) {
	// Header claims exactly maxMsgSize — should not be rejected by size check.
	// Will fail on ReadFull (not enough data), but that's a different error.
	var buf bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], maxMsgSize)
	buf.Write(hdr[:])
	var got Response
	err := readMsg(&buf, &got)
	if err == nil {
		t.Fatal("expected error (not enough data)")
	}
	if strings.Contains(err.Error(), "too large") {
		t.Error("maxMsgSize should be accepted, not rejected as too large")
	}
}

func TestReadMsg_EmptyReader(t *testing.T) {
	var got Response
	err := readMsg(bytes.NewReader(nil), &got)
	if err == nil {
		t.Fatal("readMsg should fail on empty reader")
	}
}

func TestReadMsg_ShortHeader(t *testing.T) {
	var got Response
	err := readMsg(bytes.NewReader([]byte{0, 0}), &got)
	if err == nil {
		t.Fatal("readMsg should fail on short header")
	}
}

func TestReadMsg_MalformedJSON(t *testing.T) {
	body := []byte("not json")
	var buf bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(body)))
	buf.Write(hdr[:])
	buf.Write(body)
	var got Response
	err := readMsg(&buf, &got)
	if err == nil {
		t.Fatal("readMsg should fail on malformed JSON")
	}
}

func TestReadMsg_ZeroLength(t *testing.T) {
	var buf bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], 0)
	buf.Write(hdr[:])
	var got Response
	err := readMsg(&buf, &got)
	if err == nil {
		t.Fatal("readMsg should fail on zero-length message (empty JSON)")
	}
}

func TestWriteMsg_MarshalError(t *testing.T) {
	// Channels are not JSON-serializable.
	ch := make(chan int)
	err := writeMsg(io.Discard, ch)
	if err == nil {
		t.Fatal("writeMsg should fail on non-serializable type")
	}
}

func TestErrResp(t *testing.T) {
	r := errResp("something broke")
	if r.OK {
		t.Error("errResp should not be OK")
	}
	if r.Error != "something broke" {
		t.Errorf("Error = %q, want %q", r.Error, "something broke")
	}
}

func TestOkResp(t *testing.T) {
	r := okResp()
	if !r.OK {
		t.Error("okResp should be OK")
	}
	if r.Error != "" {
		t.Errorf("Error = %q, want empty", r.Error)
	}
}
