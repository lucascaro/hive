package tmux

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input        string
		wantMajor    int
		wantMinor    int
		wantErrMatch bool
	}{
		{"tmux 3.4", 3, 4, false},
		{"tmux 3.4a", 3, 4, false},  // letter suffix ignored
		{"tmux 3.2", 3, 2, false},
		{"tmux 3.1", 3, 1, false},
		{"tmux 2.9", 2, 9, false},
		{"tmux 4.0", 4, 0, false},
		{"tmux 3.3b", 3, 3, false},
		{"", 0, 0, true},
		{"notmux", 0, 0, true},
		{"tmux only-one-field", 0, 0, true},
	}

	for _, tc := range tests {
		major, minor, err := parseVersion(tc.input)
		if tc.wantErrMatch {
			if err == nil {
				t.Errorf("parseVersion(%q): expected error, got major=%d minor=%d", tc.input, major, minor)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseVersion(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if major != tc.wantMajor || minor != tc.wantMinor {
			t.Errorf("parseVersion(%q) = (%d, %d), want (%d, %d)",
				tc.input, major, minor, tc.wantMajor, tc.wantMinor)
		}
	}
}

func TestSupportsDisplayPopup(t *testing.T) {
	tests := []struct {
		major int
		minor int
		want  bool
	}{
		{3, 2, true},  // exact minimum
		{3, 3, true},
		{3, 4, true},
		{4, 0, true},
		{4, 1, true},
		{3, 1, false}, // one minor below minimum
		{3, 0, false},
		{2, 9, false},
		{2, 0, false},
		{1, 0, false},
	}

	for _, tc := range tests {
		got := supportsDisplayPopup(tc.major, tc.minor)
		if got != tc.want {
			t.Errorf("supportsDisplayPopup(%d, %d) = %v, want %v",
				tc.major, tc.minor, got, tc.want)
		}
	}
}
