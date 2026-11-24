// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/lucascaro/hive/internal/dictation"
	"github.com/lucascaro/hive/internal/wire"
)

// The dictation box on Windows (spec 575).
//
// macOS hosts the box in the menu bar app; Windows has none, so every
// GUI window hosts one. All of them run the same internal/dictation
// controller over their own daemon connection; only the window that
// holds the system-wide chord ever opens a box (dictation.Owner).
//
// Everything native lives on one goroutine locked to its OS thread: the
// hotkey registration (RegisterHotKey is thread-affine), the box window
// and its message loop. Wails gives no hook into its own window
// procedure, so the box is a plain Win32 popup with a multi-line edit,
// built from user32 calls.
//
// Threading rule: the HUD calls View.Show/Hide with its lock held, and
// the window's own handlers call into the HUD. So View methods never
// touch the window: they copy the state under winBox.mu and post a
// message, and the window thread applies it. Setting the edit's text
// synchronously would fire EN_CHANGE into HUD.TextChanged, which takes
// the lock already held, and deadlock.

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassExW  = user32.NewProc("RegisterClassExW")
	pCreateWindowExW   = user32.NewProc("CreateWindowExW")
	pDefWindowProcW    = user32.NewProc("DefWindowProcW")
	pCallWindowProcW   = user32.NewProc("CallWindowProcW")
	pSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	pGetMessageW       = user32.NewProc("GetMessageW")
	pTranslateMessage  = user32.NewProc("TranslateMessage")
	pDispatchMessageW  = user32.NewProc("DispatchMessageW")
	pPostMessageW      = user32.NewProc("PostMessageW")
	pSendMessageW      = user32.NewProc("SendMessageW")
	pShowWindow        = user32.NewProc("ShowWindow")
	pSetWindowPos      = user32.NewProc("SetWindowPos")
	pMoveWindow        = user32.NewProc("MoveWindow")
	pSetWindowTextW    = user32.NewProc("SetWindowTextW")
	pGetWindowTextW    = user32.NewProc("GetWindowTextW")
	pGetWindowTextLenW = user32.NewProc("GetWindowTextLengthW")
	pSetForegroundWin  = user32.NewProc("SetForegroundWindow")
	pGetForegroundWin  = user32.NewProc("GetForegroundWindow")
	pSetFocus          = user32.NewProc("SetFocus")
	pIsWindowVisible   = user32.NewProc("IsWindowVisible")
	pRegisterHotKey    = user32.NewProc("RegisterHotKey")
	pMapVirtualKeyW    = user32.NewProc("MapVirtualKeyW")
	pUnregisterHotKey  = user32.NewProc("UnregisterHotKey")
	pSetTimer          = user32.NewProc("SetTimer")
	pKillTimer         = user32.NewProc("KillTimer")
	pGetAsyncKeyState  = user32.NewProc("GetAsyncKeyState")
	pGetCursorPos      = user32.NewProc("GetCursorPos")
	pMonitorFromPoint  = user32.NewProc("MonitorFromPoint")
	pGetMonitorInfoW   = user32.NewProc("GetMonitorInfoW")
	pGetDpiForWindow   = user32.NewProc("GetDpiForWindow")
	pDestroyWindow     = user32.NewProc("DestroyWindow")
	pPostQuitMessage   = user32.NewProc("PostQuitMessage")
	pLoadCursorW       = user32.NewProc("LoadCursorW")
	pGetSysColorBrush  = user32.NewProc("GetSysColorBrush")
	pCreateFontW       = gdi32.NewProc("CreateFontW")
	pSetBkColor        = gdi32.NewProc("SetBkColor")
	pGetSysColor       = user32.NewProc("GetSysColor")
	pDeleteObject      = gdi32.NewProc("DeleteObject")
	pGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmDestroy        = 0x0002
	wmClose          = 0x0010
	wmSetFont        = 0x0030
	wmKeyDown        = 0x0100
	wmChar           = 0x0102
	wmCommand        = 0x0111
	wmCtlColorStatic = 0x0138
	wmTimer          = 0x0113
	wmPaste          = 0x0302
	wmHotkey         = 0x0312
	wmApp            = 0x8000

	wmAppShow   = wmApp + 1
	wmAppHide   = wmApp + 2
	wmAppHotkey = wmApp + 3
	wmAppQuit   = wmApp + 4

	enChange = 0x0300

	emSetSel     = 0x00B1
	emReplaceSel = 0x00C2

	wsPopup       = 0x80000000
	wsChild       = 0x40000000
	wsVisible     = 0x10000000
	wsBorder      = 0x00800000
	wsVScroll     = 0x00200000
	wsClipChild   = 0x02000000
	wsExTopmost   = 0x00000008
	wsExToolWin   = 0x00000080
	wsExClientEdg = 0x00000200

	esMultiline   = 0x0004
	esAutoVScroll = 0x0040
	esWantReturn  = 0x1000

	ssLeft          = 0x0000
	ssEndEllipsis   = 0x4000
	ssNoPrefix      = 0x0080
	swHide          = 0
	swShowNormal    = 1
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	gwlpWndProc     = ^uintptr(3) // -4
	vkShift         = 0x10
	vkControl       = 0x11
	vkMenu          = 0x12
	colorWindow     = 5
	idcArrow        = 32512
	monitorNearest  = 2
	retryTimerID    = 1
	retryIntervalMS = 2000
	hotkeyID        = 0x4856 // "HV"
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   windows.Handle
	Icon       windows.Handle
	Cursor     windows.Handle
	Background windows.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     windows.Handle
}

type winPoint struct{ X, Y int32 }

type winMsg struct {
	Hwnd    windows.HWND
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      winPoint
	Private uint32
}

type winRect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	Size    uint32
	Monitor winRect
	Work    winRect
	Flags   uint32
}

// One box per process, reached from the window procedures, which are
// plain functions. Both callbacks are made once: Windows callbacks are
// never freed and their number is capped.
var (
	theBoxForProc atomic.Pointer[winBox]
	// theBox is the running box, for stopDictation. One GUI per process.
	theBox       *winBox
	boxWndProc   = windows.NewCallback(boxProc)
	editWndProc  = windows.NewCallback(editProc)
	boxClassName = windows.StringToUTF16Ptr("HiveDictationBox")
)

type winBox struct {
	hud    *dictation.HUD
	keys   *dictation.Hotkeys
	owner  *dictation.Owner
	cancel context.CancelFunc
	done   chan struct{}

	// Window thread only.
	hwnd, target, edit, status, hints windows.HWND
	origEdit                          uintptr
	font                              uintptr
	prevFG                            uintptr
	settingText                       bool // a programmatic SetWindowText: no EN_CHANGE to the HUD
	pasted                            bool // the next EN_CHANGE comes from a paste

	// Shared with the HUD's and the connection's goroutines.
	mu      sync.Mutex
	state   dictation.State
	clear   bool
	focus   bool
	restore bool
	chord   wire.GlobalHotkey
}

// startDictation brings up the box for this window. Best effort: a
// failure is logged and the window works as before.
func (a *App) startDictation() {
	// Everything the window thread reads is built before it starts.
	b := &winBox{done: make(chan struct{})}
	conn := &dictation.Conn{Dial: a.dictationDial, MinContract: dictation.WindowsContract}
	b.hud = dictation.NewHUD(b, conn, dictation.NewExecHandy(nil), dictation.PCKeys)
	b.keys = dictation.NewHotkeys(b, nil)
	conn.HUD, conn.Hotkeys = b.hud, b.keys
	ready := make(chan error, 1)
	go b.run(ready)
	if err := <-ready; err != nil {
		log.Printf("hivegui: dictation box unavailable: %v", err)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	theBox = b
	go conn.Run(ctx)
}

// stopDictation cancels a recording the box started, lets go of the
// chord and ends the window thread.
func (a *App) stopDictation() {
	b := theBox
	if b == nil {
		return
	}
	b.cancel()
	b.hud.Shutdown()
	b.post(wmAppQuit, 0)
	<-b.done
}

// Show implements dictation.View.
func (b *winBox) Show(st dictation.State, clear, focus bool) {
	b.mu.Lock()
	b.state = st
	b.clear = b.clear || clear
	b.focus = b.focus || focus
	b.mu.Unlock()
	b.post(wmAppShow, 0)
}

// Hide implements dictation.View.
func (b *winBox) Hide(restore bool) {
	b.mu.Lock()
	b.restore = restore
	b.mu.Unlock()
	b.post(wmAppHide, 0)
}

// Register implements dictation.Registrar. Called from the connection's
// goroutine; the window thread does the registering.
func (b *winBox) Register(h wire.GlobalHotkey) {
	b.mu.Lock()
	b.chord = h
	b.mu.Unlock()
	b.post(wmAppHotkey, 0)
}

func (b *winBox) post(msg uint32, w uintptr) {
	pPostMessageW.Call(uintptr(b.hwnd), uintptr(msg), w, 0)
}

func (b *winBox) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(b.done)

	inst, _, _ := pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
	bg, _, _ := pGetSysColorBrush.Call(colorWindow)
	wc := wndClassEx{
		WndProc:    boxWndProc,
		Instance:   windows.Handle(inst),
		Cursor:     windows.Handle(cursor),
		Background: windows.Handle(bg),
		ClassName:  boxClassName,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	// A second registration in the same process fails harmlessly; the
	// class from the first is the same one.
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, err := pCreateWindowExW.Call(
		wsExTopmost|wsExToolWin, uintptr(unsafe.Pointer(boxClassName)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Dictate into a session"))),
		wsPopup|wsBorder|wsClipChild, 0, 0, 560, 200, 0, 0, inst, 0)
	if hwnd == 0 {
		ready <- fmt.Errorf("CreateWindowEx: %v", err)
		return
	}
	b.hwnd = windows.HWND(hwnd)
	theBoxForProc.Store(b)
	child := func(class string, style, exStyle uintptr) windows.HWND {
		h, _, _ := pCreateWindowExW.Call(exStyle, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(class))), 0,
			wsChild|wsVisible|style, 0, 0, 10, 10, hwnd, 0, inst, 0)
		return windows.HWND(h)
	}
	b.target = child("STATIC", ssLeft|ssEndEllipsis|ssNoPrefix, 0)
	b.edit = child("EDIT", esMultiline|esAutoVScroll|esWantReturn|wsVScroll, wsExClientEdg)
	b.status = child("STATIC", ssLeft|ssEndEllipsis|ssNoPrefix, 0)
	b.hints = child("STATIC", ssLeft|ssEndEllipsis|ssNoPrefix, 0)
	b.origEdit, _, _ = pSetWindowLongPtrW.Call(uintptr(b.edit), gwlpWndProc, editWndProc)

	b.owner = dictation.NewOwner(b.registerHotkey, b.unregisterHotkey, func(h wire.GlobalHotkey, ok bool) {
		b.keys.Registered(h, ok)
	})
	ready <- nil

	var m winMsg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (b *winBox) registerHotkey(h wire.GlobalHotkey) bool {
	mods, vk, ok := dictation.WindowsHotkey(h, layoutVK)
	if !ok {
		return false
	}
	r, _, _ := pRegisterHotKey.Call(uintptr(b.hwnd), hotkeyID, uintptr(mods), uintptr(vk))
	return r != 0
}

// layoutVK is the current keyboard layout's VK for a physical key, so a
// chord stays on the key it names on macOS (MAPVK_VSC_TO_VK_EX).
func layoutVK(scan uint32) uint32 {
	r, _, _ := pMapVirtualKeyW.Call(uintptr(scan), 3)
	return uint32(r)
}

func (b *winBox) unregisterHotkey() {
	pUnregisterHotKey.Call(uintptr(b.hwnd), hotkeyID)
}

// syncRetryTimer runs the retry timer exactly while the chord is wanted
// and another window (or app) holds it.
func (b *winBox) syncRetryTimer() {
	if b.owner.Retrying() {
		pSetTimer.Call(uintptr(b.hwnd), retryTimerID, retryIntervalMS, 0)
	} else {
		pKillTimer.Call(uintptr(b.hwnd), retryTimerID)
	}
}

func boxProc(hwnd, msg, wp, lp uintptr) uintptr {
	b := theBoxForProc.Load()
	if b == nil || windows.HWND(hwnd) != b.hwnd {
		r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
		return r
	}
	switch msg {
	case wmHotkey:
		if wp == hotkeyID {
			if fg, _, _ := pGetForegroundWin.Call(); fg != uintptr(b.hwnd) {
				b.prevFG = fg
			}
			b.hud.Open()
		}
		return 0
	case wmAppShow:
		b.applyShow()
		return 0
	case wmAppHide:
		b.mu.Lock()
		restore := b.restore
		b.mu.Unlock()
		pShowWindow.Call(hwnd, swHide)
		if restore && b.prevFG != 0 {
			pSetForegroundWin.Call(b.prevFG)
		}
		b.prevFG = 0
		return 0
	case wmAppHotkey:
		b.mu.Lock()
		h := b.chord
		b.mu.Unlock()
		b.owner.Want(h)
		b.syncRetryTimer()
		return 0
	case wmTimer:
		if wp == retryTimerID {
			b.owner.Tick()
			b.syncRetryTimer()
		}
		return 0
	case wmCommand:
		if windows.HWND(lp) == b.edit && wp>>16 == enChange && !b.settingText {
			pasted := b.pasted
			b.pasted = false
			b.hud.TextChanged(dictation.FromEdit(b.editText()), pasted)
		}
		return 0
	case wmCtlColorStatic:
		// The labels share the box's background rather than the dialog
		// grey a STATIC paints by default.
		c, _, _ := pGetSysColor.Call(colorWindow)
		pSetBkColor.Call(wp, c)
		br, _, _ := pGetSysColorBrush.Call(colorWindow)
		return br
	case wmClose:
		b.hud.Cancel()
		return 0
	case wmAppQuit:
		pKillTimer.Call(hwnd, retryTimerID)
		b.owner.Release()
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		if b.font != 0 {
			pDeleteObject.Call(b.font)
		}
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

func editProc(hwnd, msg, wp, lp uintptr) uintptr {
	b := theBoxForProc.Load()
	if b == nil {
		return 0
	}
	switch msg {
	case wmKeyDown:
		shift := keyDown(vkShift)
		ctrl := keyDown(vkControl)
		alt := keyDown(vkMenu)
		switch dictation.EditKeyAction(uint32(wp), shift, ctrl, alt) {
		case dictation.EditSubmit:
			b.hud.Submit(dictation.FromEdit(b.editText()))
			return 0
		case dictation.EditNewLine:
			crlf := windows.StringToUTF16Ptr("\r\n")
			pSendMessageW.Call(hwnd, emReplaceSel, 1, uintptr(unsafe.Pointer(crlf)))
			return 0
		case dictation.EditCancel:
			b.hud.Cancel()
			return 0
		case dictation.EditPrev:
			b.hud.Move(-1)
			return 0
		case dictation.EditNext:
			b.hud.Move(1)
			return 0
		}
	case wmChar:
		switch wp {
		case '\r', '\n', 0x1B:
			// The WM_KEYDOWN above already acted; the character would
			// add a second newline, or beep.
			return 0
		case 0x01: // Ctrl+A, which a plain EDIT does not select-all on
			pSendMessageW.Call(hwnd, emSetSel, 0, ^uintptr(0))
			return 0
		}
	case wmPaste:
		// Handy pastes with a synthesized Ctrl+V, which arrives here. The
		// edit reports the change (EN_CHANGE) synchronously while handling
		// it, so the flag covers exactly this paste; one that changed
		// nothing must not mark the next keystroke as pasted.
		b.pasted = true
		r, _, _ := pCallWindowProcW.Call(b.origEdit, hwnd, msg, wp, lp)
		b.pasted = false
		return r
	}
	r, _, _ := pCallWindowProcW.Call(b.origEdit, hwnd, msg, wp, lp)
	return r
}

// keyDown reports a modifier held right now. Not GetKeyState: that is
// this thread's view of the keyboard, and the hotkey's own Ctrl and
// Shift are often released while another app is still in front, so the
// box's thread never sees them come up. Plain Esc and Down then read as
// Shift+Esc and Shift+Down and did nothing (found on the Windows VM).
func keyDown(vk uintptr) bool {
	r, _, _ := pGetAsyncKeyState.Call(vk)
	return r&0x8000 != 0
}

func (b *winBox) editText() string {
	n, _, _ := pGetWindowTextLenW.Call(uintptr(b.edit))
	buf := make([]uint16, n+1)
	pGetWindowTextW.Call(uintptr(b.edit), uintptr(unsafe.Pointer(&buf[0])), n+1)
	return windows.UTF16ToString(buf)
}

func setText(h windows.HWND, s string) {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil { // a NUL in a title
		p = windows.StringToUTF16Ptr("")
	}
	pSetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(p)))
}

// targetLine is the box's first line: where the text will go.
func targetLine(st dictation.State) string {
	if st.Selected < 0 || st.Selected >= len(st.Targets) {
		return "To: (no session selected)"
	}
	if len(st.Targets) == 1 {
		return "To: " + st.Targets[st.Selected].Label
	}
	return fmt.Sprintf("To: %s   (%d of %d)", st.Targets[st.Selected].Label, st.Selected+1, len(st.Targets))
}

func (b *winBox) applyShow() {
	b.mu.Lock()
	st, clear, focus := b.state, b.clear, b.focus
	b.clear, b.focus = false, false
	b.mu.Unlock()

	setText(b.target, targetLine(st))
	setText(b.status, st.Status)
	setText(b.hints, st.Hints)
	if clear {
		b.settingText = true
		setText(b.edit, "")
		b.settingText = false
		b.pasted = false
	}
	if v, _, _ := pIsWindowVisible.Call(uintptr(b.hwnd)); v == 0 {
		b.place()
		pShowWindow.Call(uintptr(b.hwnd), swShowNormal)
	}
	if focus {
		pSetForegroundWin.Call(uintptr(b.hwnd))
		pSetFocus.Call(uintptr(b.edit))
		pSendMessageW.Call(uintptr(b.edit), emSetSel, ^uintptr(0), ^uintptr(0))
	}
}

// place centres the box on the monitor under the pointer, sized for
// that monitor's DPI.
func (b *winBox) place() {
	var pt winPoint
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	mon, _, _ := pMonitorFromPoint.Call(uintptr(*(*uint64)(unsafe.Pointer(&pt))), monitorNearest)
	mi := monitorInfo{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	// Move onto the monitor first so GetDpiForWindow answers for it.
	pSetWindowPos.Call(uintptr(b.hwnd), 0, uintptr(mi.Work.Left), uintptr(mi.Work.Top), 0, 0, swpNoZOrder|swpNoActivate|0x0001)
	dpi, _, _ := pGetDpiForWindow.Call(uintptr(b.hwnd))
	if dpi == 0 {
		dpi = 96
	}
	s := func(v int) int32 { return int32(v * int(dpi) / 96) }
	w, pad, line, editH := s(560), s(12), s(22), s(96)
	h := pad + line + s(6) + editH + s(6) + line + line + pad
	x := mi.Work.Left + (mi.Work.Right-mi.Work.Left-w)/2
	y := mi.Work.Top + (mi.Work.Bottom-mi.Work.Top-h)/3
	pSetWindowPos.Call(uintptr(b.hwnd), 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoZOrder|swpNoActivate)

	if b.font != 0 {
		pDeleteObject.Call(b.font)
	}
	b.font, _, _ = pCreateFontW.Call(uintptr(-s(15)), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Segoe UI"))))
	inner := w - 2*pad
	yy := pad
	for _, c := range []struct {
		h      windows.HWND
		height int32
	}{{b.target, line}, {b.edit, editH}, {b.status, line}, {b.hints, line}} {
		pMoveWindow.Call(uintptr(c.h), uintptr(pad), uintptr(yy), uintptr(inner), uintptr(c.height), 1)
		pSendMessageW.Call(uintptr(c.h), wmSetFont, b.font, 1)
		yy += c.height
		if c.h == b.target || c.h == b.edit {
			yy += s(6)
		}
	}
}
