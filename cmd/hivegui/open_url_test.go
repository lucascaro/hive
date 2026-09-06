package main

import "testing"

func TestAllowedURL(t *testing.T) {
	ok := []string{
		"https://github.com/lucascaro/hive",
		"http://localhost:5173/",
		"HTTPS://Example.com",
		"mailto:someone@example.com",
	}
	bad := []string{
		"",
		"file:///Applications/Utilities/Terminal.app",
		"javascript:alert(1)",
		"vscode://file/etc/passwd",
		"x-apple.systempreferences:com.apple.preference.security",
		"ssh://evil.example",
		"github.com/no/scheme",
		"https://\x00bad",
	}
	for _, u := range ok {
		if !allowedURL(u) {
			t.Errorf("allowedURL(%q) = false, want true", u)
		}
	}
	for _, u := range bad {
		if allowedURL(u) {
			t.Errorf("allowedURL(%q) = true, want false", u)
		}
	}
}
