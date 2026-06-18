// Opt the process out of macOS App Nap / activity-based throttling.
//
// -[NSProcessInfo beginActivityWithOptions:reason:] returns a token that
// must be retained for as long as the assertion should hold. We stash it in
// a static (retained under ARC) and never end it — the assertion lasts the
// whole process lifetime, which is what a always-live terminal needs.
//
//   NSActivityUserInitiated     — treat as user-driven work, not background.
//   NSActivityLatencyCritical   — disable timer coalescing/throttling; this
//                                 is the bit that addresses the ~1 Hz clamp.

#import <Foundation/Foundation.h>

static id<NSObject> gActivityToken = nil;

void hiveBeginActivity(void) {
    if (gActivityToken != nil) {
        return;
    }
    NSActivityOptions opts = NSActivityUserInitiated | NSActivityLatencyCritical;
    gActivityToken = [[NSProcessInfo processInfo]
        beginActivityWithOptions:opts
                          reason:@"Hive keeps PTY sessions streaming and the terminal painting while not frontmost"];
}
