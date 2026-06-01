package client

import (
	"bytes"
	"testing"
)

func feed(s *DetachScanner, in []byte) (out []byte, detached bool) {
	var buf bytes.Buffer
	for _, b := range in {
		fwd, det := s.Push(b)
		buf.Write(fwd)
		if det {
			return buf.Bytes(), true
		}
	}
	return buf.Bytes(), false
}

func TestDetach_PlainBytesPassThrough(t *testing.T) {
	out, det := feed(&DetachScanner{}, []byte("hello"))
	if det || !bytes.Equal(out, []byte("hello")) {
		t.Fatalf("got (%q,%v)", out, det)
	}
}

func TestDetach_CtrlADTriggersDetach(t *testing.T) {
	out, det := feed(&DetachScanner{}, []byte{0x01, 'd'})
	if !det {
		t.Fatal("expected detach on Ctrl-A d")
	}
	if len(out) != 0 {
		t.Fatalf("detach must not forward bytes, got %q", out)
	}
}

func TestDetach_DoubleCtrlAForwardsLiteral(t *testing.T) {
	out, det := feed(&DetachScanner{}, []byte{0x01, 0x01})
	if det || !bytes.Equal(out, []byte{0x01}) {
		t.Fatalf("got (%q,%v), want single 0x01", out, det)
	}
}

func TestDetach_CtrlAThenOtherForwardsBoth(t *testing.T) {
	out, det := feed(&DetachScanner{}, []byte{0x01, 'x'})
	if det || !bytes.Equal(out, []byte{0x01, 'x'}) {
		t.Fatalf("got (%q,%v), want 0x01 'x'", out, det)
	}
}
