package tmux

import (
	"testing"
)

func TestParsePaneTitles(t *testing.T) {
	cases := []struct {
		name       string
		input      string
		wantTitles map[string]string
		wantBells  map[string]bool
	}{
		{
			name:  "basic with bell",
			input: "0" + paneSep + "my title" + paneSep + "1",
			wantTitles: map[string]string{
				"sess:0": "my title",
			},
			wantBells: map[string]bool{
				"sess:0": true,
			},
		},
		{
			name:  "no bell",
			input: "0" + paneSep + "my title" + paneSep + "0",
			wantTitles: map[string]string{
				"sess:0": "my title",
			},
			wantBells: map[string]bool{},
		},
		{
			name: "multiple windows",
			input: "0" + paneSep + "first" + paneSep + "0\n" +
				"1" + paneSep + "second" + paneSep + "1\n" +
				"2" + paneSep + "third" + paneSep + "0",
			wantTitles: map[string]string{
				"sess:0": "first",
				"sess:1": "second",
				"sess:2": "third",
			},
			wantBells: map[string]bool{
				"sess:1": true,
			},
		},
		{
			name:  "title containing tabs",
			input: "0" + paneSep + "title\twith\ttabs" + paneSep + "1",
			wantTitles: map[string]string{
				"sess:0": "title\twith\ttabs",
			},
			wantBells: map[string]bool{
				"sess:0": true,
			},
		},
		{
			name:       "empty input",
			input:      "",
			wantTitles: map[string]string{},
			wantBells:  map[string]bool{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			titles, bells, err := parsePaneTitles("sess", tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(titles) != len(tc.wantTitles) {
				t.Errorf("titles: got %d entries, want %d", len(titles), len(tc.wantTitles))
			}
			for k, want := range tc.wantTitles {
				if got := titles[k]; got != want {
					t.Errorf("titles[%q] = %q, want %q", k, got, want)
				}
			}
			if len(bells) != len(tc.wantBells) {
				t.Errorf("bells: got %d entries, want %d", len(bells), len(tc.wantBells))
			}
			for k, want := range tc.wantBells {
				if got := bells[k]; got != want {
					t.Errorf("bells[%q] = %v, want %v", k, got, want)
				}
			}
		})
	}
}
