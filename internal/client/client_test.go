package client

import "testing"

func TestVersionStringIsNonEmpty(t *testing.T) {
	if ClientName() == "" {
		t.Fatal("ClientName() must not be empty")
	}
}
