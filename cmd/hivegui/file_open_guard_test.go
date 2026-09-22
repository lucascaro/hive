package main

import "testing"

// TestIsLaunchable is the guard's whole contract, and it runs on every
// CI runner for every platform — that is why isLaunchable takes goos
// as an argument. A ⌘-click that opens one of these files is arbitrary
// code execution from terminal output someone else wrote.
func TestIsLaunchable(t *testing.T) {
	cases := []struct {
		name string
		goos string
		path string
		meta fileMeta
		want bool
	}{
		// --- macOS ---
		{"darwin app bundle", "darwin", "/Applications/Utilities/Terminal.app", fileMeta{isDir: true}, true},
		{"darwin prefpane mixed case", "darwin", "/x/Some.PrefPane", fileMeta{isDir: true}, true},
		{"darwin plain directory", "darwin", "/Users/x/src", fileMeta{isDir: true}, false},
		{"darwin command file", "darwin", "/x/run.command", fileMeta{}, true},
		{"darwin exec bit", "darwin", "/x/deploy", fileMeta{execBit: true}, true},
		{"darwin terminal profile", "darwin", "/x/evil.terminal", fileMeta{}, true},
		{"darwin webloc", "darwin", "/x/link.webloc", fileMeta{}, true},
		{"darwin python", "darwin", "/x/setup.py", fileMeta{}, true},
		{"darwin shell script no exec bit", "darwin", "/x/install.sh", fileMeta{}, true},
		{"darwin jnlp", "darwin", "/x/app.jnlp", fileMeta{}, true},
		{"darwin finder alias", "darwin", "/x/notes", fileMeta{isAlias: true}, true},
		{"darwin disk image", "darwin", "/x/payload.iso", fileMeta{}, true},
		{"darwin sparse image", "darwin", "/x/payload.sparseimage", fileMeta{}, true},
		{"darwin xip archive", "darwin", "/x/payload.xip", fileMeta{}, true},
		{"darwin config profile", "darwin", "/x/evil.mobileconfig", fileMeta{}, true},
		{"darwin webarchive", "darwin", "/x/evil.webarchive", fileMeta{}, true},
		{"darwin markdown", "darwin", "/x/README.md", fileMeta{}, false},
		{"darwin typescript", "darwin", "/x/src/foo.ts", fileMeta{}, false},
		{"darwin dotted name", "darwin", "/x/report.v2.md", fileMeta{}, false},
		{"darwin backup of a script", "darwin", "/x/setup.py.bak", fileMeta{}, false},

		// --- Linux ---
		{"linux desktop entry", "linux", "/x/evil.desktop", fileMeta{}, true},
		{"linux appimage mixed case", "linux", "/x/Tool.AppImage", fileMeta{}, true},
		{"linux exec bit", "linux", "/x/deploy", fileMeta{execBit: true}, true},
		{"linux python", "linux", "/x/setup.py", fileMeta{}, true},
		{"linux text", "linux", "/x/notes.txt", fileMeta{}, false},
		{"linux directory", "linux", "/x/src", fileMeta{isDir: true}, false},
		{"linux app-named directory", "linux", "/x/thing.app", fileMeta{isDir: true}, false},

		// --- Windows ---
		{"windows exe", "windows", `C:\x\evil.exe`, fileMeta{}, true},
		{"windows bat uppercase", "windows", `C:\x\EVIL.BAT`, fileMeta{}, true},
		{"windows compiled help", "windows", `C:\x\payload.chm`, fileMeta{}, true},
		{"windows excel add-in", "windows", `C:\x\payload.xll`, fileMeta{}, true},
		{"windows scriptlet", "windows", `C:\x\payload.wsc`, fileMeta{}, true},
		{"windows autohotkey", "windows", `C:\x\payload.ahk`, fileMeta{}, true},
		{"windows mounted image", "windows", `C:\x\payload.iso`, fileMeta{}, true},
		{"windows shortcut", "windows", `C:\x\evil.lnk`, fileMeta{}, true},
		{"windows powershell", "windows", `C:\x\evil.ps1`, fileMeta{}, true},
		{"windows url file", "windows", `C:\x\evil.url`, fileMeta{}, true},
		{"windows settingcontent", "windows", `C:\x\payload.settingcontent-ms`, fileMeta{}, true},
		{"windows python", "windows", `C:\x\setup.py`, fileMeta{}, true},
		{"windows double extension", "windows", `C:\x\notes.txt.exe`, fileMeta{}, true},
		// Win32 strips a trailing dot or space before opening, so these
		// three all reach evil.exe while filepath.Ext reads clean.
		{"windows trailing dot", "windows", `C:\x\evil.exe.`, fileMeta{}, true},
		{"windows trailing space", "windows", `C:\x\evil.exe `, fileMeta{}, true},
		{"windows alternate data stream", "windows", `C:\x\evil.exe::$DATA`, fileMeta{}, true},
		{"windows stream on a text file", "windows", `C:\x\notes.txt::$DATA`, fileMeta{}, true},
		{"windows text", "windows", `C:\x\notes.txt`, fileMeta{}, false},
		{"windows dotted name", "windows", `C:\x\report.v2.md`, fileMeta{}, false},
		{"windows backup of a script", "windows", `C:\x\setup.py.bak`, fileMeta{}, false},
		{"windows drive-rooted text file", "windows", `C:\notes.txt`, fileMeta{}, false},
		{"windows exec bit is meaningless", "windows", `C:\x\notes.txt`, fileMeta{execBit: true}, false},
		{"windows directory", "windows", `C:\x\src`, fileMeta{isDir: true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isLaunchable(tc.goos, tc.path, tc.meta); got != tc.want {
				t.Fatalf("isLaunchable(%q, %q, %+v) = %v, want %v", tc.goos, tc.path, tc.meta, got, tc.want)
			}
		})
	}
}
