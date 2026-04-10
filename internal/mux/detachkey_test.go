package mux

import "testing"

func TestParseDetachKey_Valid(t *testing.T) {
	cases := []struct {
		in   string
		want DetachKeySpec
	}{
		{"ctrl+q", DetachKeySpec{Raw: "ctrl+q", Display: "Ctrl+Q", Tmux: "C-q", Byte: 0x11}},
		{"ctrl+d", DetachKeySpec{Raw: "ctrl+d", Display: "Ctrl+D", Tmux: "C-d", Byte: 0x04}},
		{"ctrl+a", DetachKeySpec{Raw: "ctrl+a", Display: "Ctrl+A", Tmux: "C-a", Byte: 0x01}},
		{"ctrl+z", DetachKeySpec{Raw: "ctrl+z", Display: "Ctrl+Z", Tmux: "C-z", Byte: 0x1a}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseDetachKey(tc.in)
			if err != nil {
				t.Fatalf("ParseDetachKey(%q) returned error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseDetachKey(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseDetachKey_Invalid(t *testing.T) {
	cases := []string{
		"",              // empty
		"q",             // missing modifier
		"ctrl+Q",        // uppercase letter
		"alt+d",         // unsupported modifier
		"shift+d",       // unsupported modifier
		"meta+d",        // unsupported modifier
		"ctrl+f1",       // multi-character key
		"ctrl+shift+q",  // multi-modifier (parses as 3 parts)
		"ctrl+1",        // digit not letter
		"ctrl+",         // missing key
		"+q",            // missing modifier
		"ctrl+ ",        // space
		"ctrl",          // no plus
		"CTRL+q",        // uppercase modifier
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := ParseDetachKey(in); err == nil {
				t.Errorf("ParseDetachKey(%q) returned no error, expected one", in)
			}
		})
	}
}

func TestDefaultDetachKey(t *testing.T) {
	if DefaultDetachKey != "ctrl+q" {
		t.Errorf("DefaultDetachKey = %q, want %q", DefaultDetachKey, "ctrl+q")
	}
	if _, err := ParseDetachKey(DefaultDetachKey); err != nil {
		t.Errorf("DefaultDetachKey %q must be parseable: %v", DefaultDetachKey, err)
	}
}
