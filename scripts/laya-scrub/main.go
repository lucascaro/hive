// Command laya-scrub redacts secrets from captured screens before they
// join the Laya corpus (internal/laya/testdata/corpus/README.md). It
// rewrites each file in place and exits non-zero if anything still
// matches a secret pattern afterwards.
//
//	go run ./scripts/laya-scrub <file-or-dir>...
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/lucascaro/hive/internal/laya"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: laya-scrub <file-or-dir>...")
		os.Exit(2)
	}
	dirty := false
	for _, root := range os.Args[1:] {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".txt") {
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			clean := laya.Scrub(string(raw))
			if clean != string(raw) {
				if err := os.WriteFile(path, []byte(clean), 0o600); err != nil {
					return err
				}
				fmt.Println("scrubbed", path)
			}
			if found := laya.Secrets(clean); len(found) != 0 {
				dirty = true
				fmt.Printf("STILL CONTAINS %v: %s\n", found, path)
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if dirty {
		os.Exit(1)
	}
}
