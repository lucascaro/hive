package session

import "testing"

func TestShellEscape(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"claude"}, "claude"},
		{[]string{"claude", "--help"}, "claude --help"},
		{[]string{"echo", "hello world"}, `echo 'hello world'`},
		{[]string{"echo", `it's`}, `echo 'it'\''s'`},
		{[]string{"echo", `$HOME`}, `echo '$HOME'`},
		{[]string{"foo", ""}, `foo ''`},
		{[]string{"prog", "a;rm -rf /"}, `prog 'a;rm -rf /'`},
	}
	for _, tc := range cases {
		got := shellEscape(tc.argv)
		if got != tc.want {
			t.Errorf("shellEscape(%v) = %q, want %q", tc.argv, got, tc.want)
		}
	}
}
