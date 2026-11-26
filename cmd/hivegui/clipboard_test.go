// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"errors"
	"testing"
)

func TestPickClipboard(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name              string
		hasText, hasImage bool
		text              string
		readErr           error
		want              ClipboardContents
		wantErr           error
		wantRead          bool
	}{
		// The bug: an image-only clipboard must not read text (the read
		// fails with ERROR_SUCCESS on Windows) and must report the image.
		{name: "image only", hasImage: true, want: ClipboardContents{Image: true}},
		{name: "empty", want: ClipboardContents{}},
		{name: "text", hasText: true, text: "hi", want: ClipboardContents{Text: "hi"}, wantRead: true},
		// Text wins over an image: a paste of both is a text paste.
		{name: "text and image", hasText: true, hasImage: true, text: "hi", want: ClipboardContents{Text: "hi"}, wantRead: true},
		{name: "real read error", hasText: true, readErr: boom, wantErr: boom, wantRead: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			read := false
			got, err := pickClipboard(c.hasText, c.hasImage, func() (string, error) {
				read = true
				return c.text, c.readErr
			})
			if !errors.Is(err, c.wantErr) || got != c.want || read != c.wantRead {
				t.Fatalf("got (%+v, %v, read=%v), want (%+v, %v, read=%v)",
					got, err, read, c.want, c.wantErr, c.wantRead)
			}
		})
	}
}
