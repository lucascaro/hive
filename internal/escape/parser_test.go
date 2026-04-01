package escape

import "testing"

func TestExtractTitle(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "no escape sequences",
			input: "just plain text",
			want:  "",
		},
		{
			name:  "OSC 2 sequence",
			input: "\x1b]2;my title\x07",
			want:  "my title",
		},
		{
			name:  "OSC 2 multiple matches returns last",
			input: "\x1b]2;first\x07some text\x1b]2;second\x07",
			want:  "second",
		},
		{
			name:  "null byte HIVE_TITLE marker",
			input: "\x00HIVE_TITLE:agent title\x00",
			want:  "agent title",
		},
		{
			name:  "null byte marker multiple matches returns last",
			input: "\x00HIVE_TITLE:first\x00stuff\x00HIVE_TITLE:last\x00",
			want:  "last",
		},
		{
			name:  "null byte marker preferred over OSC 2",
			input: "\x1b]2;osc title\x07\x00HIVE_TITLE:marker title\x00",
			want:  "marker title",
		},
		{
			name:  "null byte marker preferred even when OSC 2 comes after",
			input: "\x00HIVE_TITLE:marker\x00\x1b]2;osc\x07",
			want:  "marker",
		},
		{
			name:  "OSC 2 title with spaces",
			input: "\x1b]2;hello world\x07",
			want:  "hello world",
		},
		{
			name:  "HIVE_TITLE with spaces",
			input: "\x00HIVE_TITLE:building feature X\x00",
			want:  "building feature X",
		},
		{
			name:  "surrounding noise",
			input: "some output\nmore output\n\x1b]2;the title\x07\nmore output",
			want:  "the title",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractTitle(tc.input)
			if got != tc.want {
				t.Errorf("ExtractTitle(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
