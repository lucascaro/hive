package session

import (
	"bytes"
	"math/rand"
	"testing"
)

// refRing is a verbatim transcription of the pre-circular-buffer
// appendRing implementation (grow-then-make+copy). It is the oracle the
// circular ring is measured against: the replay path must hand back
// exactly the same bytes in exactly the same order as this did, for
// every write sequence, or a reattaching xterm.js repaints garbage.
//
// Do not "improve" this. Its only job is to be the old behaviour.
type refRing struct {
	capacity int
	buf      []byte
}

func (r *refRing) append(p []byte) {
	if len(p) == 0 {
		return
	}
	r.buf = append(r.buf, p...)
	if len(r.buf) <= r.capacity {
		return
	}
	drop := len(r.buf) - r.capacity
	const scanWindow = 4 << 10
	limit := drop + scanWindow
	if limit > len(r.buf) {
		limit = len(r.buf)
	}
	safe := drop
	for i := drop; i < limit; i++ {
		b := r.buf[i]
		if b == 0x0A || b == 0x0D {
			safe = i
			break
		}
	}
	if safe == drop {
		const backScan = 64
		for i := drop; i < limit; i++ {
			b := r.buf[i]
			if b == 0x1B {
				safe = i
				break
			}
			if b < 0x80 || (b&0xC0) == 0xC0 {
				if !insideUnterminatedEscape(r.buf, i, backScan) {
					safe = i
					break
				}
			}
		}
	}
	retained := len(r.buf) - safe
	next := make([]byte, retained)
	copy(next, r.buf[safe:])
	r.buf = next
}

// newRingVT builds a VT good enough to exercise the byte ring without
// standing up the vt10x emulator: appendRing/ringBytes/ReplayBytes touch
// only ring state. Tests drive appendRing directly so an 8 MiB stream
// costs a memcpy instead of a full terminal parse.
func newRingVT() *VT { return &VT{} }

func fill(n int, b byte) []byte { return bytes.Repeat([]byte{b}, n) }

// TestRing_BelowCapKeepsEveryByteInOrder pins the simplest contract: until
// the ring fills, read-back is the exact concatenation of every write.
func TestRing_BelowCapKeepsEveryByteInOrder(t *testing.T) {
	v := newRingVT()
	var want bytes.Buffer
	chunks := [][]byte{
		[]byte("hello "),
		[]byte("\x1b[31mred\x1b[m "),
		[]byte("world\r\n"),
		{}, // empty writes must be no-ops
		[]byte("\xe2\x9c\x93 done\r\n"),
		fill(1<<20, 'x'),
		[]byte("tail"),
	}
	for _, c := range chunks {
		v.appendRing(c)
		want.Write(c)
	}
	if got := v.ringBytes(); !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("ring mismatch: got %d bytes, want %d bytes", len(got), want.Len())
	}
}

// TestRing_ExactlyAtCapacityDoesNotTrim pins the boundary: a stream of
// exactly ringCap bytes is retained whole, and the very next byte is what
// starts the trimming.
func TestRing_ExactlyAtCapacityDoesNotTrim(t *testing.T) {
	v := newRingVT()
	want := fill(ringCap, 'A')
	v.appendRing(want[:ringCap-1])
	v.appendRing(want[ringCap-1:])
	got := v.ringBytes()
	if len(got) != ringCap {
		t.Fatalf("len after exactly ringCap = %d, want %d", len(got), ringCap)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("content mismatch at exactly ringCap")
	}

	// One more byte: no newline/ESC anywhere in the scan window, so the
	// trim drops exactly the overflow and nothing else.
	v.appendRing([]byte("B"))
	got = v.ringBytes()
	if len(got) != ringCap {
		t.Fatalf("len after ringCap+1 = %d, want %d", len(got), ringCap)
	}
	if !bytes.Equal(got, append(fill(ringCap-1, 'A'), 'B')) {
		t.Fatalf("content mismatch after one-byte overflow: first=%q last=%q", got[0], got[len(got)-1])
	}
}

// TestRing_OverflowWithNoSafeBoundary pins the fallback: when the whole
// scan window is UTF-8 continuation bytes (never a valid replay start),
// the ring drops exactly the overflow and nothing more.
func TestRing_OverflowWithNoSafeBoundary(t *testing.T) {
	v := newRingVT()
	stream := fill(ringCap+1000, 0x80) // continuation bytes only
	v.appendRing(stream)
	got := v.ringBytes()
	if len(got) != ringCap {
		t.Fatalf("len = %d, want %d", len(got), ringCap)
	}
	if !bytes.Equal(got, stream[1000:]) {
		t.Fatalf("content mismatch: expected the last ringCap bytes verbatim")
	}
}

// TestRing_BackScanSeesDroppedContext is the sharpest test in this file.
//
// The overflow point lands 3 bytes into "\x1b[31m", i.e. at the '1'. The
// boundary scan must reject '1' and 'm' because a back-scan over bytes
// that are BEING DROPPED finds the unterminated CSI they belong to, and
// settle on the first byte after the 'm'. An implementation that cannot
// see behind the retained region (the obvious circular-buffer mistake)
// accepts '1' immediately and starts replay with a literal "1m".
func TestRing_BackScanSeesDroppedContext(t *testing.T) {
	const lead = 100 // > 64 so the back-scan window is fully populated
	v := newRingVT()
	v.appendRing(fill(lead, 'A'))
	v.appendRing([]byte("\x1b[31m"))
	v.appendRing(fill(ringCap-2, 'A'))

	got := v.ringBytes()
	want := fill(ringCap-2, 'A')
	if !bytes.Equal(got, want) {
		head := got
		if len(head) > 8 {
			head = head[:8]
		}
		t.Fatalf("ring should start after the CSI final byte; got len=%d head=%q, want len=%d of 'A'",
			len(got), head, len(want))
	}
}

// TestRing_BackScanContextIsClampedToRingStart pins the other half of the
// same rule: the back-scan may look behind the retained region only as far
// as bytes dropped by THIS write. Here only 3 bytes precede the overflow
// point, and they are the CSI itself, so the scan still sees it.
func TestRing_BackScanContextIsClampedToRingStart(t *testing.T) {
	v := newRingVT()
	v.appendRing([]byte("\x1b[31m"))
	v.appendRing(fill(ringCap-2, 'A'))
	if got, want := v.ringBytes(), fill(ringCap-2, 'A'); !bytes.Equal(got, want) {
		t.Fatalf("got len=%d, want len=%d", len(got), len(want))
	}
}

// TestRing_WriteLargerThanWholeRing pins the single-write-overflows-
// everything case, including a write several times the ring's size.
func TestRing_WriteLargerThanWholeRing(t *testing.T) {
	for _, size := range []int{ringCap + 1, ringCap + 4096, 2*ringCap + 777} {
		v := newRingVT()
		ref := &refRing{capacity: ringCap}
		stream := make([]byte, size)
		for i := range stream {
			stream[i] = byte('a' + i%26)
		}
		v.appendRing([]byte("seed\r\n"))
		ref.append([]byte("seed\r\n"))
		v.appendRing(stream)
		ref.append(stream)

		got := v.ringBytes()
		if len(got) > ringCap {
			t.Fatalf("size %d: ring exceeds cap: %d", size, len(got))
		}
		if !bytes.Equal(got, ref.buf) {
			t.Fatalf("size %d: ring != reference (got %d bytes, want %d)", size, len(got), len(ref.buf))
		}
		if !bytes.HasSuffix(stream, got) {
			t.Fatalf("size %d: ring is not a suffix of the written stream", size)
		}
	}
}

// TestRing_SteadyStateOverflowMatchesReference walks the ring far past
// capacity with small ConPTY-sized writes — the state every long-lived
// session ends up in — and demands byte-exact agreement with the old
// implementation after every single write.
func TestRing_SteadyStateOverflowMatchesReference(t *testing.T) {
	streams := map[string]func(i int) []byte{
		"newline-rich": func(i int) []byte {
			return []byte("\x1b[32mline " + string(rune('a'+i%26)) + "\x1b[m\r\n")
		},
		"no-newlines": func(i int) []byte {
			return []byte("\x1b[38;5;" + string(rune('0'+i%10)) + "mABC\xe2\x9c\x93")
		},
		"osc-titles": func(i int) []byte {
			return []byte("\x1b]0;title " + string(rune('a'+i%26)) + "\x07plain text")
		},
	}
	for name, gen := range streams {
		t.Run(name, func(t *testing.T) {
			v := newRingVT()
			ref := &refRing{capacity: ringCap}
			// Get to the brink in one cheap write, then grind across it.
			seed := fill(ringCap-3000, 'A')
			v.appendRing(seed)
			ref.append(seed)
			for i := 0; i < 120; i++ {
				c := gen(i)
				v.appendRing(c)
				ref.append(c)
				if got := v.ringBytes(); !bytes.Equal(got, ref.buf) {
					t.Fatalf("write %d: ring != reference (len %d vs %d)", i, len(got), len(ref.buf))
				}
			}
		})
	}
}

// TestRing_RandomizedAgainstReference fuzzes chunk sizes and byte values
// (escapes, UTF-8 lead/continuation bytes, newlines, CSI finals) across
// the overflow point.
func TestRing_RandomizedAgainstReference(t *testing.T) {
	alphabet := []byte{0x1B, '[', ']', '0', '1', ';', 'm', 'h', 0x07, 'A', 'z', ' ', '\r', '\n', 0xE2, 0x9C, 0x93, 0x80, 0xBF, 0xC3}
	for _, seed := range []int64{1, 7, 42} {
		rng := rand.New(rand.NewSource(seed))
		v := newRingVT()
		ref := &refRing{capacity: ringCap}
		pre := fill(ringCap-1500, 'A')
		v.appendRing(pre)
		ref.append(pre)
		for i := 0; i < 80; i++ {
			n := 1 + rng.Intn(700)
			chunk := make([]byte, n)
			for j := range chunk {
				chunk[j] = alphabet[rng.Intn(len(alphabet))]
			}
			v.appendRing(chunk)
			ref.append(chunk)
			if got := v.ringBytes(); !bytes.Equal(got, ref.buf) {
				t.Fatalf("seed %d write %d (len %d): ring != reference (len %d vs %d)",
					seed, i, n, len(got), len(ref.buf))
			}
		}
	}
}

// TestRing_ReplayBytesMatchesRingAfterOverflow pins the actual read path
// used on reattach: ReplayBytes is the ring, in order, followed by the DEC
// mode restore — still true once the ring has wrapped.
func TestRing_ReplayBytesMatchesRingAfterOverflow(t *testing.T) {
	v := NewVT(80, 24)
	if _, err := v.Write([]byte("\x1b[?2004h\x1b[?1h")); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Overflow via appendRing directly: the emulator has already seen the
	// mode bytes above, and parsing 8 MiB is not what this test is about.
	v.mu.Lock()
	v.appendRing(fill(ringCap+4096, 'A'))
	v.mu.Unlock()

	ring := v.ringBytes()
	if len(ring) != ringCap {
		t.Fatalf("ring len = %d, want %d", len(ring), ringCap)
	}
	replay := v.ReplayBytes()
	if !bytes.HasPrefix(replay, ring) {
		t.Fatalf("ReplayBytes does not start with the ring")
	}
	rest := replay[len(ring):]
	if len(rest) == 0 || rest[0] != 0x18 {
		t.Fatalf("ReplayBytes: expected CAN after the ring, got %q", rest)
	}
	if !bytes.Contains(rest, []byte("\x1b[?2004h")) {
		t.Fatalf("ReplayBytes lost the DEC mode restore: %q", rest)
	}
}

// TestRing_ReadBackIsADefensiveCopy pins that a caller holding a previous
// read-back is unaffected by later writes — a circular buffer that handed
// out an internal slice would mutate it under them.
func TestRing_ReadBackIsADefensiveCopy(t *testing.T) {
	v := newRingVT()
	v.appendRing(fill(ringCap, 'A'))
	first := v.ringBytes()
	snapshot := append([]byte(nil), first...)
	v.appendRing(fill(4096, 'B'))
	if !bytes.Equal(first, snapshot) {
		t.Fatalf("previously returned ring bytes were mutated by a later write")
	}
	first[0] = 'Z'
	if v.ringBytes()[0] == 'Z' {
		t.Fatalf("mutating the returned slice changed the ring")
	}
}

// TestRing_EmptyReadBack pins the nil-not-empty contract callers branch on.
func TestRing_EmptyReadBack(t *testing.T) {
	v := newRingVT()
	if got := v.ringBytes(); got != nil {
		t.Fatalf("ringBytes on a fresh VT = %q, want nil", got)
	}
	v.appendRing(nil)
	if got := v.ringBytes(); got != nil {
		t.Fatalf("ringBytes after an empty write = %q, want nil", got)
	}
}

// --- benchmarks -----------------------------------------------------------

// conPTYChunk is the size of a typical ConPTY read on Windows: one line of
// agent output. The ring is hit once per chunk, so this is the size that
// decides whether a long-lived session stays usable.
var conPTYChunk = []byte("2026-09-13T12:00:00Z  \x1b[32mok\x1b[m  finished step 1234 of a long build\r\n")

// primeOverflow drives a ring to the steady state every long-lived
// session ends up in: full, and already past its first overflow, so the
// benchmark measures the per-write cost and not one-time setup.
func primeOverflow() *VT {
	v := newRingVT()
	v.appendRing(fill(ringCap, 'A'))
	v.appendRing(fill(4096, 'A')) // no boundary bytes: drops exactly 4096
	return v
}

func BenchmarkAppendRing(b *testing.B) {
	b.Run("below_cap_69B", func(b *testing.B) {
		v := newRingVT()
		written := 0
		b.SetBytes(int64(len(conPTYChunk)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if written+len(conPTYChunk) > ringCap {
				b.StopTimer()
				v = newRingVT()
				written = 0
				b.StartTimer()
			}
			v.appendRing(conPTYChunk)
			written += len(conPTYChunk)
		}
	})

	b.Run("at_cap_69B", func(b *testing.B) {
		v := primeOverflow()
		b.SetBytes(int64(len(conPTYChunk)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			v.appendRing(conPTYChunk)
		}
	})

	b.Run("at_cap_64KiB", func(b *testing.B) {
		chunk := fill(64<<10, 'q')
		copy(chunk[1000:], "\r\n")
		v := primeOverflow()
		b.SetBytes(int64(len(chunk)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			v.appendRing(chunk)
		}
	})

	b.Run("past_cap_oversized_write", func(b *testing.B) {
		chunk := fill(ringCap+4096, 'z')
		v := primeOverflow()
		b.SetBytes(int64(len(chunk)))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			v.appendRing(chunk)
		}
	})
}
