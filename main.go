package main

import (
	_ "embed"

	"github.com/lucascaro/hive/cmd"
)

//go:embed CHANGELOG.md
var changelog string

func main() {
	cmd.EmbeddedChangelog = changelog
	cmd.Execute()
}
