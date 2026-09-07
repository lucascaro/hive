package main

import "testing"

func TestRemoteIsUpstream(t *testing.T) {
	ok := []string{
		"https://github.com/lucascaro/hive",
		"https://github.com/lucascaro/hive.git",
		"https://github.com/lucascaro/hive/",
		"git@github.com:lucascaro/hive.git",
		"ssh://git@github.com/lucascaro/hive.git",
		"https://GitHub.com/lucascaro/hive",
	}
	bad := []string{
		"https://evil.example/lucascaro/hive",
		"https://github.com.evil.example/lucascaro/hive",
		"https://github.com/lucascaro/hive-fork",
		"https://github.com/someone/lucascaro/hive",
		"git@gitlab.com:lucascaro/hive.git",
		"",
	}
	for _, u := range ok {
		if !remoteIsUpstream(u) {
			t.Errorf("%q: want true", u)
		}
	}
	for _, u := range bad {
		if remoteIsUpstream(u) {
			t.Errorf("%q: want false", u)
		}
	}
}
