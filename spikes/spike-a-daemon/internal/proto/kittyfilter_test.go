package proto

import (
	"bytes"
	"testing"
)

func TestKittyFilter(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want []byte
	}{
		{
			name: "passthrough",
			in:   []byte("hello world"),
			want: []byte("hello world"),
		},
		{
			name: "strip kitty enable push",
			in:   []byte("before\x1b[>1uafter"),
			want: []byte("beforeafter"),
		},
		{
			name: "strip kitty enable with flags",
			in:   []byte("\x1b[>15;1u$"),
			want: []byte("$"),
		},
		{
			name: "strip kitty disable pop",
			in:   []byte("x\x1b[<uy"),
			want: []byte("xy"),
		},
		{
			name: "strip kitty set",
			in:   []byte("\x1b[=7;1ux"),
			want: []byte("x"),
		},
		{
			name: "strip kitty query",
			in:   []byte("\x1b[?ux"),
			want: []byte("x"),
		},
		{
			name: "preserve unrelated CSI (cursor up)",
			in:   []byte("\x1b[2A"),
			want: []byte("\x1b[2A"),
		},
		{
			name: "preserve DECSET (private mode set, ends in h)",
			in:   []byte("\x1b[?1049h"),
			want: []byte("\x1b[?1049h"),
		},
		{
			name: "preserve DECRST (ends in l)",
			in:   []byte("\x1b[?1049l"),
			want: []byte("\x1b[?1049l"),
		},
		{
			name: "isolated ESC",
			in:   []byte("\x1bX"),
			want: []byte("\x1bX"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &KittyFilter{}
			got := f.Filter(tc.in)
			if !bytes.Equal(got, tc.want) {
				t.Errorf("Filter(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestKittyFilterSplitAcrossChunks(t *testing.T) {
	// Sequence "\x1b[>1u" arrives one byte at a time.
	f := &KittyFilter{}
	chunks := [][]byte{{0x1b}, {'['}, {'>'}, {'1'}, {'u'}, {'X'}}
	var got []byte
	for _, c := range chunks {
		got = append(got, f.Filter(c)...)
	}
	if !bytes.Equal(got, []byte("X")) {
		t.Errorf("split-stream filter dropped wrong bytes: got %q", got)
	}
}
