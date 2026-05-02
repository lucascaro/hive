// Native macOS notifications. Uses NSUserNotification rather than
// osascript so the toast shows the Hive bundle's icon, supports a
// subtitle, and lets us route clicks back to Go.
//
// NSUserNotification is deprecated since macOS 11 in favor of the
// UserNotifications framework, but it still works through current
// macOS versions and avoids the entitlement/authorization dance that
// UNUserNotificationCenter requires for unsigned dev builds.

#import <Cocoa/Cocoa.h>

// Defined in notify_darwin.go via //export.
extern void hiveOnNotificationActivated(const char *tag);

@interface HiveNotificationDelegate : NSObject <NSUserNotificationCenterDelegate>
@end

@implementation HiveNotificationDelegate

#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"

- (void)userNotificationCenter:(NSUserNotificationCenter *)center
       didActivateNotification:(NSUserNotification *)notification {
    NSString *tag = notification.userInfo[@"tag"];
    // Bring Hive to the foreground regardless of which Space the user
    // is on; the click implicitly says "I want to go there".
    [NSApp activateIgnoringOtherApps:YES];
    if (tag != nil) {
        hiveOnNotificationActivated([tag UTF8String]);
    }
}

// Without this, NSUserNotification suppresses the banner whenever the
// app is foreground — but we want to alert on bells from non-focused
// sessions even while the Hive window itself is focused.
- (BOOL)userNotificationCenter:(NSUserNotificationCenter *)center
     shouldPresentNotification:(NSUserNotification *)notification {
    return YES;
}

#pragma clang diagnostic pop

@end

static HiveNotificationDelegate *gDelegate = nil;

void hiveInstallNotifyDelegate(void) {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
    if (gDelegate == nil) {
        gDelegate = [[HiveNotificationDelegate alloc] init];
        [NSUserNotificationCenter defaultUserNotificationCenter].delegate = gDelegate;
    }
#pragma clang diagnostic pop
}

void hivePostNotification(const char *title,
                          const char *subtitle,
                          const char *body,
                          const char *tag) {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
    NSUserNotification *n = [[NSUserNotification alloc] init];
    if (title != NULL) {
        n.title = [NSString stringWithUTF8String:title];
    }
    if (subtitle != NULL && strlen(subtitle) > 0) {
        n.subtitle = [NSString stringWithUTF8String:subtitle];
    }
    if (body != NULL) {
        n.informativeText = [NSString stringWithUTF8String:body];
    }
    if (tag != NULL && strlen(tag) > 0) {
        NSString *t = [NSString stringWithUTF8String:tag];
        n.userInfo = @{ @"tag": t };
        // Setting the identifier lets macOS replace prior notifications
        // with the same id rather than stacking them, which is exactly
        // what we want for repeated bells from the same session.
        n.identifier = t;
    }
    [[NSUserNotificationCenter defaultUserNotificationCenter] deliverNotification:n];
#pragma clang diagnostic pop
}
