package wire

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		ftype   FrameType
		payload []byte
	}{
		{"empty", FrameData, nil},
		{"small", FrameData, []byte("hello")},
		{"binary", FrameData, []byte{0x00, 0x01, 0xff, 0xfe, 0x7f}},
		{"control", FrameHello, []byte(`{"version":0}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteFrame(&buf, tc.ftype, tc.payload); err != nil {
				t.Fatalf("WriteFrame: %v", err)
			}
			gotType, gotPayload, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}
			if gotType != tc.ftype {
				t.Errorf("type: got %s, want %s", gotType, tc.ftype)
			}
			if !bytes.Equal(gotPayload, tc.payload) {
				t.Errorf("payload mismatch: got %q, want %q", gotPayload, tc.payload)
			}
		})
	}
}

func TestFrameTooLargeOnWrite(t *testing.T) {
	var buf bytes.Buffer
	big := make([]byte, MaxPayload+1)
	if err := WriteFrame(&buf, FrameData, big); !errors.Is(err, ErrFrameTooLarge) {
		t.Errorf("got %v, want ErrFrameTooLarge", err)
	}
}

func TestFrameTooLargeOnRead(t *testing.T) {
	// Forge a header that claims an absurd payload length.
	hdr := []byte{byte(FrameData), 0xff, 0xff, 0xff, 0xff}
	_, _, err := ReadFrame(bytes.NewReader(hdr))
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Errorf("got %v, want ErrFrameTooLarge", err)
	}
}

func TestReadFrameTruncated(t *testing.T) {
	var buf bytes.Buffer
	_ = WriteFrame(&buf, FrameData, []byte("hello"))
	// Truncate mid-payload.
	truncated := buf.Bytes()[:7]
	_, _, err := ReadFrame(bytes.NewReader(truncated))
	if err == nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("got %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestHelloWelcomeRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, FrameHello, Hello{Version: 0, Client: "hive/0.1.0"}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	if err := WriteJSON(&buf, FrameWelcome, Welcome{Version: 0, SessionID: "abc", Cols: 80, Rows: 24}); err != nil {
		t.Fatalf("write welcome: %v", err)
	}

	var hello Hello
	if ft, err := ReadJSON(&buf, &hello); err != nil || ft != FrameHello {
		t.Fatalf("read hello: ft=%s err=%v", ft, err)
	}
	if hello.Client != "hive/0.1.0" || hello.Version != 0 {
		t.Errorf("hello = %+v", hello)
	}

	var welcome Welcome
	if ft, err := ReadJSON(&buf, &welcome); err != nil || ft != FrameWelcome {
		t.Fatalf("read welcome: ft=%s err=%v", ft, err)
	}
	if welcome.SessionID != "abc" || welcome.Cols != 80 || welcome.Rows != 24 {
		t.Errorf("welcome = %+v", welcome)
	}
}

func TestFrameTypeStringUnknown(t *testing.T) {
	s := FrameType(0xab).String()
	if !strings.Contains(s, "0xab") {
		t.Errorf("unknown stringer = %q", s)
	}
}
