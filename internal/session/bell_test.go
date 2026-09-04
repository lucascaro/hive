package session

import "testing"

func TestBellScanner(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"plain text", "hello world\n", false},
		{"bare bell", "done\x07", true},

		// The case the scanner exists for. Every shell prompt sets a
		// title, so counting this 0x07 would mark every session as
		// wanting attention forever.
		{"osc title terminated by BEL", "\x1b]2;~/src/hive\x07$ ", false},
		{"osc title terminated by ST", "\x1b]2;~/src/hive\x1b\\$ ", false},
		{"osc title then a real bell", "\x1b]2;t\x07ding\x07", true},

		// A BEL inside the title TEXT is impossible (it would terminate
		// the sequence), but an ESC inside it is not — that must not be
		// mistaken for the start of ST.
		{"osc containing a literal ESC", "\x1b]2;a\x1bb\x07x", false},

		{"csi colour run", "\x1b[31mred\x1b[0m", false},
		{"csi then bell", "\x1b[31mred\x1b[0m\x07", true},
		{"short escape", "\x1b7text\x1b8", false},
		{"short escape then bell", "\x1b7\x07", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var s bellScanner
			if got := s.Scan([]byte(c.input)); got != c.want {
				t.Errorf("Scan(%q) = %v, want %v", c.input, got, c.want)
			}
		})
	}
}

// A PTY read can split a sequence anywhere. The scanner is stateful
// precisely so a title cut in half does not surface its terminator as
// a bell — a per-chunk scan gets this wrong, and it is the failure that
// would show up only under load.
func TestBellScannerAcrossChunkBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		chunks []string
		want   bool
	}{
		{
			name:   "osc split before its terminator",
			chunks: []string{"\x1b]2;~/src/hi", "ve\x07$ "},
			want:   false,
		},
		{
			name:   "osc split mid-introducer",
			chunks: []string{"\x1b", "]2;t\x07"},
			want:   false,
		},
		{
			name:   "osc split immediately before ST",
			chunks: []string{"\x1b]2;t\x1b", "\\"},
			want:   false,
		},
		{
			name:   "real bell in a later chunk",
			chunks: []string{"\x1b]2;t\x07", "output", "\x07"},
			want:   true,
		},
		{
			name:   "csi split across chunks then a bell",
			chunks: []string{"\x1b[3", "1m", "\x07"},
			want:   true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var s bellScanner
			got := false
			for _, chunk := range c.chunks {
				if s.Scan([]byte(chunk)) {
					got = true
				}
			}
			if got != c.want {
				t.Errorf("scanning %q = %v, want %v", c.chunks, got, c.want)
			}
		})
	}
}

// Two bells in one chunk are one request for attention, not two.
func TestBellScannerReportsOncePerChunk(t *testing.T) {
	var s bellScanner
	if !s.Scan([]byte("\x07\x07\x07")) {
		t.Fatal("want true")
	}
	if s.Scan([]byte("quiet")) {
		t.Error("state leaked across chunks: a quiet chunk reported a bell")
	}
}
