// hivegui is the desktop client for hived. See
// docs/native-rewrite/phase-1.md for its role in the native rewrite.
package main

import (
	"embed"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

// resolveLaunchDir is best-effort capture of the user's intent for
// "where should new sessions open". macOS launches .app bundles with
// cwd "/" regardless of how they were invoked (open, Finder, even
// running Contents/MacOS/<bin> directly), so os.Getwd alone is not
// reliable. We try, in order:
//  1. os.Getwd() if it isn't "" or "/"
//  2. $PWD env var (preserved when the user ran the binary directly
//     from a shell that exports PWD)
//  3. $HOME
func resolveLaunchDir() string {
	if cwd, err := os.Getwd(); err == nil && cwd != "" && cwd != "/" {
		return cwd
	}
	if pwd := os.Getenv("PWD"); pwd != "" && pwd != "/" {
		return pwd
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}

func main() {
	launchDir := resolveLaunchDir()
	app := NewApp(launchDir)
	err := wails.Run(&options.App{
		Title:            "Hive",
		Width:            1024,
		Height:           700,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 1},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
	})
	if err != nil {
		println("hivegui:", err.Error())
	}
}
