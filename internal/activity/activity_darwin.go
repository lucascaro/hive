package activity

/*
#cgo darwin CFLAGS: -x objective-c -fobjc-arc
#cgo darwin LDFLAGS: -framework Foundation
void hiveBeginActivity(void);
*/
import "C"

func disableThrottling() { C.hiveBeginActivity() }
