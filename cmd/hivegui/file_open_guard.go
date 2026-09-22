// The launchable-file guard behind ⌘-click on a path in a session.
//
// Terminal content is attacker-influenced — an agent prints it, a
// `cat` of a README prints it, and an OSC 8 link can label any URI
// with any visible text. So a click must never hand the OS something
// it would *run*. isLaunchable decides that, and OpenFile reveals
// anything it flags instead of opening it (see file_open.go).
//
// The whole decision is a pure function of (goos, path, fileMeta) and
// lives here rather than in the per-GOOS files, so every OS's table is
// exercised by `go test` on every CI runner. staticcheck only analyses
// one GOOS at a time, and a Windows-only test body is dead weight on
// the Linux leg — which is exactly how a guard grows a hole nobody
// sees. The per-GOOS files hold only the side-effecting parts
// (openDefault, reveal, statMeta).
package main

import (
	"path/filepath"
	"strings"
)

// fileMeta is what the per-GOOS statMeta could learn about a path.
// Everything the guard needs beyond the name itself.
type fileMeta struct {
	isDir   bool
	execBit bool // any of the three x bits (unix only; always false on Windows)
	isAlias bool // macOS Finder alias: not a symlink, but `open` follows it
}

// Extensions the OS may execute on *any* platform, because the
// handler is an interpreter rather than a viewer. A python.org install
// makes Python Launcher the default app for .py on macOS; .py and .pyw
// run through py.exe on Windows; a .jar runs wherever a JRE is
// installed. Opening one of these is running it.
var interpreterExts = map[string]bool{
	".py": true, ".pyw": true, ".pyz": true,
	".pl": true, ".rb": true, ".php": true,
	".sh": true, ".bash": true, ".zsh": true, ".fish": true,
	".csh": true, ".ksh": true,
	".jar": true, ".jnlp": true,
	".ps1": true, ".psm1": true,
	".applescript": true, ".scpt": true, ".scptd": true,
	".command": true, ".tool": true,
}

// macOS: directories the Finder treats as a single launchable object.
var darwinBundleExts = map[string]bool{
	".app": true, ".bundle": true, ".framework": true, ".plugin": true,
	".prefPane": true, ".saver": true, ".workflow": true, ".action": true,
	".kext": true, ".qlgenerator": true, ".appex": true,
}

// macOS: files whose default handler acts on them rather than showing
// them. .webloc/.inetloc/.fileloc are saved links, .pkg/.mpkg run the
// Installer, .dmg mounts a volume.
var darwinFileExts = map[string]bool{
	".terminal": true, ".webloc": true, ".inetloc": true, ".fileloc": true,
	".pkg": true, ".mpkg": true, ".dmg": true, ".shortcut": true,
	// DiskImageMounter mounts all of these exactly as it mounts .dmg.
	".iso": true, ".cdr": true, ".img": true,
	".sparseimage": true, ".sparsebundle": true,
	// .xip expands a signed archive, .mobileconfig opens the
	// configuration-profile installer, and .webarchive opens in Safari
	// with a file:// origin.
	".xip": true, ".mobileconfig": true, ".webarchive": true,
}

var linuxExts = map[string]bool{
	".desktop": true, ".appimage": true, ".run": true,
	".deb": true, ".rpm": true, ".flatpakref": true, ".snap": true,
}

// Windows has no exec bit, so the extension is the whole decision.
// This is PATHEXT plus every extension whose registered handler
// executes: script hosts, installers, shortcuts and the control-panel
// family. .settingcontent-ms and .library-ms are here because both
// have been used as code-execution vectors.
var windowsExts = map[string]bool{
	".exe": true, ".com": true, ".bat": true, ".cmd": true,
	".vbs": true, ".vbe": true, ".js": true, ".jse": true,
	".wsf": true, ".wsh": true, ".hta": true,
	".msi": true, ".msp": true, ".msc": true,
	".scr": true, ".cpl": true, ".lnk": true, ".url": true,
	".pif": true, ".reg": true, ".inf": true, ".scf": true,
	".appref-ms": true, ".application": true, ".gadget": true,
	".settingcontent-ms": true, ".library-ms": true, ".diagcab": true,
	// Script hosts and add-in loaders whose handler executes the file:
	// hh.exe runs .chm, Excel loads .xll as a DLL, .wsc/.sct are
	// scriptlets, AutoHotkey runs .ahk.
	".chm": true, ".xll": true, ".xlam": true, ".ppam": true,
	".ahk": true, ".wsc": true, ".sct": true, ".ws": true,
	".search-ms": true, ".msix": true, ".appx": true,
	// Explorer mounts these, which is how a payload gets a drive letter.
	".iso": true, ".img": true, ".vhd": true, ".vhdx": true,
}

// isLaunchable reports whether opening path with the OS default
// handler could *run* something. It fails safe: anything it cannot
// reason about confidently is launchable, because the cost of a false
// positive is a Finder window and the cost of a false negative is
// arbitrary code execution.
//
// goos is a parameter rather than runtime.GOOS so the table for every
// platform is testable from any host — see the package comment above.
func isLaunchable(goos, path string, meta fileMeta) bool {
	if meta.isAlias {
		// A Finder alias is not a symlink, so EvalSymlinks does not
		// resolve it and we cannot cheaply see what it points at.
		// `open` follows it, which may well land on an .app.
		return true
	}
	if goos == "windows" && suspiciousWindowsName(path) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	if meta.isDir {
		// Only macOS has launchable directories. Elsewhere a
		// directory is just a directory (OpenFile reveals it).
		return goos == "darwin" && darwinBundleExts[normalizeBundleExt(ext)]
	}
	if interpreterExts[ext] {
		return true
	}
	switch goos {
	case "darwin":
		return meta.execBit || darwinFileExts[ext]
	case "windows":
		return windowsExts[ext]
	default:
		return meta.execBit || linuxExts[ext]
	}
}

// normalizeBundleExt maps a lowercased extension back onto the
// canonical spelling used in darwinBundleExts (".prefPane" is the only
// one with an uppercase letter). Comparing lowercased on both sides
// would be simpler, but the map doubles as documentation of the real
// names.
func normalizeBundleExt(lowerExt string) string {
	if lowerExt == ".prefpane" {
		return ".prefPane"
	}
	return lowerExt
}

// suspiciousWindowsName reports whether a path plays one of the Win32
// name games that make an extension check lie.
//
//   - Win32 strips trailing dots and spaces before opening a file, so
//     `evil.exe.` and `evil.exe ` both reach evil.exe while
//     filepath.Ext returns "." and ".exe " respectively.
//   - NTFS alternate data streams: `evil.exe::$DATA` opens the same
//     bytes, and the extension reads as ".exe::$DATA".
//
// Both are pure name tricks, so they are caught here rather than in
// the per-GOOS statMeta, and they are caught for *every* file — a
// trailing dot on a .txt is still a name we refuse to reason about.
// 8.3 short names (PAYLOA~1.SET) are handled the other way round: the
// Windows statMeta expands them before the guard runs, because the
// long name is the one whose handler actually fires.
func suspiciousWindowsName(path string) bool {
	base := path
	if i := strings.LastIndexAny(base, `\/`); i >= 0 {
		base = base[i+1:]
	}
	if base == "" {
		return false
	}
	if strings.ContainsAny(base, ":") {
		return true // alternate data stream; a drive letter never reaches here
	}
	last := base[len(base)-1]
	return last == '.' || last == ' '
}
