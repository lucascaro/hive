// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The dictation HUD's native layer (spec 553): a Carbon hotkey and a
// floating panel. It decides nothing. Keys go to Go (hud.go), and the
// panel shows whatever Go last sent. Every entry point hops to the main
// thread, and every callback into Go only records an event and returns,
// so the main thread never waits on a socket.
//
// Why these APIs (verified on macOS 26 with a throwaway spike before
// this was written):
//   - RegisterEventHotKey fires with another app in front and needs no
//     Accessibility permission. An event tap or an NSEvent global
//     monitor would prompt for it.
//   - A non-activating NSPanel that can become key takes keystrokes and
//     dictation without activating the app. Activating first
//     (activateIgnoringOtherApps) made focus arrive late and the panel
//     lag for seconds.
//   - Dictation only reaches the field when the process runs as an .app
//     bundle, which hivebar always does.

#import <Cocoa/Cocoa.h>
#import <Carbon/Carbon.h>

#include "hud_darwin.h"
#include "_cgo_export.h"

// ---------- the hotkey ----------

static EventHotKeyRef hbHotkeyRef = NULL;
static BOOL hbHandlerInstalled = NO;

static OSStatus hbHotkeyHandler(EventHandlerCallRef next, EventRef ev, void *ud) {
  goHotkeyPressed();
  return noErr;
}

void hbHotkeySet(uint32_t keyCode, uint32_t modifiers, int enabled) {
  dispatch_async(dispatch_get_main_queue(), ^{
    if (!hbHandlerInstalled) {
      EventTypeSpec spec = {kEventClassKeyboard, kEventHotKeyPressed};
      InstallApplicationEventHandler(&hbHotkeyHandler, 1, &spec, NULL, NULL);
      hbHandlerInstalled = YES;
    }
    if (hbHotkeyRef != NULL) {
      UnregisterEventHotKey(hbHotkeyRef);
      hbHotkeyRef = NULL;
    }
    if (!enabled) {
      goHotkeyRegistered(keyCode, modifiers, 0, 1);
      return;
    }
    EventHotKeyID hid = {'hivd', 1};
    OSStatus st = RegisterEventHotKey(keyCode, modifiers, hid,
                                      GetApplicationEventTarget(), 0, &hbHotkeyRef);
    if (st != noErr) {
      hbHotkeyRef = NULL;
      NSLog(@"hivebar: RegisterEventHotKey(%u, %#x) failed: %d", keyCode, modifiers, (int)st);
    }
    goHotkeyRegistered(keyCode, modifiers, 1, st == noErr);
  });
}

// ---------- the panel ----------

@interface HBPanel : NSPanel
@end
@implementation HBPanel
- (BOOL)canBecomeKeyWindow { return YES; }
@end

@interface HBHUD : NSObject <NSTextFieldDelegate>
@property(strong) HBPanel *panel;
@property(strong) NSPopUpButton *targets;
@property(strong) NSTextField *field;
@property(strong) NSTextField *status;
@property(strong) NSTextField *hints;
@property(strong) NSRunningApplication *previous;
// The labels the popup was last built from: rebuilding an unchanged list
// would close the popup under a user who has it open.
@property(copy) NSString *builtLabels;
@end

static HBHUD *hbHUD = nil;

static NSTextField *hbLabel(NSRect frame, CGFloat size, NSColor *color) {
  NSTextField *l = [NSTextField labelWithString:@""];
  l.frame = frame;
  l.font = [NSFont systemFontOfSize:size];
  l.textColor = color;
  l.lineBreakMode = NSLineBreakByTruncatingTail;
  return l;
}

// An accessory app has no main menu, and without one ⌘V, ⌘C and ⌘A do
// nothing in a text field: they are menu key equivalents. Paste is how
// most dictation tools deliver text, so the HUD installs the Edit menu.
static void hbEnsureEditMenu(void) {
  if (NSApp.mainMenu != nil) return;
  NSMenu *main = [NSMenu new];
  NSMenuItem *appItem = [NSMenuItem new];
  appItem.submenu = [NSMenu new];
  [main addItem:appItem];
  NSMenuItem *editItem = [NSMenuItem new];
  NSMenu *edit = [[NSMenu alloc] initWithTitle:@"Edit"];
  [edit addItemWithTitle:@"Undo" action:@selector(undo:) keyEquivalent:@"z"];
  [edit addItemWithTitle:@"Redo" action:@selector(redo:) keyEquivalent:@"Z"];
  [edit addItem:[NSMenuItem separatorItem]];
  [edit addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];
  [edit addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];
  [edit addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];
  [edit addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
  editItem.submenu = edit;
  [main addItem:editItem];
  NSApp.mainMenu = main;
}

@implementation HBHUD
- (instancetype)init {
  if (!(self = [super init])) return nil;
  hbEnsureEditMenu();
  NSRect r = NSMakeRect(0, 0, 620, 176);
  self.panel = [[HBPanel alloc]
      initWithContentRect:r
                styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskNonactivatingPanel |
                           NSWindowStyleMaskFullSizeContentView)
                  backing:NSBackingStoreBuffered
                    defer:NO];
  self.panel.level = NSFloatingWindowLevel;
  self.panel.becomesKeyOnlyIfNeeded = NO;
  self.panel.hidesOnDeactivate = NO;
  self.panel.releasedWhenClosed = NO;
  self.panel.titleVisibility = NSWindowTitleHidden;
  self.panel.titlebarAppearsTransparent = YES;
  self.panel.movableByWindowBackground = YES;
  self.panel.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
                                  NSWindowCollectionBehaviorFullScreenAuxiliary;
  NSView *c = self.panel.contentView;

  NSTextField *to = hbLabel(NSMakeRect(16, 134, 80, 20), 13, NSColor.secondaryLabelColor);
  to.stringValue = @"Send to:";
  [c addSubview:to];
  self.targets = [[NSPopUpButton alloc] initWithFrame:NSMakeRect(84, 130, 520, 26) pullsDown:NO];
  self.targets.target = self;
  self.targets.action = @selector(pick:);
  // The popup must never take focus from the field: ↑↓ in the field
  // drive it instead.
  self.targets.refusesFirstResponder = YES;
  [c addSubview:self.targets];

  self.field = [[NSTextField alloc] initWithFrame:NSMakeRect(16, 52, 588, 70)];
  self.field.placeholderString = @"Dictate or type…";
  self.field.font = [NSFont systemFontOfSize:15];
  self.field.usesSingleLineMode = NO;
  self.field.cell.wraps = YES;
  self.field.cell.scrollable = NO;
  self.field.lineBreakMode = NSLineBreakByWordWrapping;
  self.field.delegate = self;
  [c addSubview:self.field];

  self.status = hbLabel(NSMakeRect(16, 28, 588, 18), 12, NSColor.systemOrangeColor);
  [c addSubview:self.status];
  self.hints = hbLabel(NSMakeRect(16, 8, 588, 18), 11, NSColor.tertiaryLabelColor);
  [c addSubview:self.hints];
  return self;
}

// Every edit goes to Go, which decides whether Handy has delivered its
// transcription (hud.go TextChanged). A paste arrives as the key event
// that triggered the Edit menu's Paste: ⌘V, which is also what Handy
// synthesizes. Programmatic changes (clearing on open) send no
// notification, so they are never mistaken for input.
- (void)controlTextDidChange:(NSNotification *)note {
  NSEvent *e = NSApp.currentEvent;
  int pasted = e.type == NSEventTypeKeyDown &&
               (e.modifierFlags & NSEventModifierFlagCommand) &&
               e.keyCode == kVK_ANSI_V;
  goHUDTextChanged((char *)self.field.stringValue.UTF8String, pasted);
}

- (void)pick:(id)sender {
  goHUDSelect((int)self.targets.indexOfSelectedItem);
  [self.panel makeFirstResponder:self.field];
}

- (BOOL)control:(NSControl *)control
               textView:(NSTextView *)tv
    doCommandBySelector:(SEL)sel {
  if (sel == @selector(insertNewline:)) {
    NSEventModifierFlags f = NSApp.currentEvent.modifierFlags;
    if (f & (NSEventModifierFlagShift | NSEventModifierFlagOption)) {
      [tv insertNewlineIgnoringFieldEditor:self];
      return YES;
    }
    goHUDSubmit((char *)self.field.stringValue.UTF8String);
    return YES;
  }
  if (sel == @selector(insertLineBreak:) || sel == @selector(insertNewlineIgnoringFieldEditor:)) {
    [tv insertNewlineIgnoringFieldEditor:self];
    return YES;
  }
  if (sel == @selector(cancelOperation:)) {
    goHUDCancel();
    return YES;
  }
  if (sel == @selector(moveUp:)) {
    goHUDMove(-1);
    return YES;
  }
  if (sel == @selector(moveDown:)) {
    goHUDMove(1);
    return YES;
  }
  return NO;
}
@end

void hbHUDShow(const char *labels, int n, int selected, const char *status,
               const char *hints, int clear, int focus) {
  NSString *joined = n > 0 ? [NSString stringWithUTF8String:labels] : @"";
  NSArray<NSString *> *items =
      n > 0 ? [joined componentsSeparatedByString:@"\x1f"] : @[];
  NSString *st = [NSString stringWithUTF8String:status];
  NSString *hi = [NSString stringWithUTF8String:hints];
  dispatch_async(dispatch_get_main_queue(), ^{
    if (hbHUD == nil) hbHUD = [HBHUD new];
    HBHUD *h = hbHUD;
    if (![joined isEqualToString:h.builtLabels]) {
      h.builtLabels = joined;
      [h.targets removeAllItems];
      for (NSString *s in items) {
        // addItemWithTitle: drops a duplicate title; two sessions can share
        // a label, so add the item and set its title directly.
        [h.targets.menu addItem:[[NSMenuItem alloc] initWithTitle:s action:nil keyEquivalent:@""]];
      }
    }
    if (selected >= 0 && selected < (int)items.count) {
      [h.targets selectItemAtIndex:selected];
    } else {
      [h.targets selectItemAtIndex:-1];
      h.targets.title = items.count ? @"(pick a session)" : @"(none)";
    }
    h.targets.enabled = items.count > 0;
    h.status.stringValue = st;
    h.hints.stringValue = hi;
    if (clear) h.field.stringValue = @"";
    BOOL opening = !h.panel.isVisible;
    if (opening) {
      NSRunningApplication *front = NSWorkspace.sharedWorkspace.frontmostApplication;
      h.previous = [front isEqual:NSRunningApplication.currentApplication] ? nil : front;
      NSScreen *screen = NSScreen.mainScreen;
      if (screen != nil) {
        NSRect sf = screen.visibleFrame;
        NSRect pf = h.panel.frame;
        [h.panel setFrameOrigin:NSMakePoint(NSMidX(sf) - pf.size.width / 2,
                                            NSMinY(sf) + sf.size.height * 0.62)];
      }
    }
    // Only a hotkey press takes focus. A live refresh (a title change, a
    // new session) leaves key status, the caret and any running dictation
    // where the user has them.
    if (!opening && !focus) return;
    [h.panel makeKeyAndOrderFront:nil];
    [h.panel makeFirstResponder:h.field];
    // Put the caret at the end rather than selecting what is there, so a
    // second dictation appends instead of replacing.
    NSText *editor = [h.panel fieldEditor:NO forObject:h.field];
    if (editor != nil) editor.selectedRange = NSMakeRange(editor.string.length, 0);
  });
}

int hbAppRunning(const char *bundleID) {
  // Called from Go worker threads, which have no autorelease pool.
  @autoreleasepool {
    NSString *bid = [NSString stringWithUTF8String:bundleID];
    return [NSRunningApplication runningApplicationsWithBundleIdentifier:bid].count > 0;
  }
}

void hbHUDHide(int restore) {
  dispatch_async(dispatch_get_main_queue(), ^{
    HBHUD *h = hbHUD;
    if (h == nil) return;
    [h.panel orderOut:nil];
    h.field.stringValue = @"";
    NSRunningApplication *prev = h.previous;
    h.previous = nil;
    if (restore && prev != nil && !prev.terminated) {
      [prev activateWithOptions:0];
    }
  });
}
