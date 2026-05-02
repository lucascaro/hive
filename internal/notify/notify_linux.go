package notify

import "os/exec"

// platformNotify uses notify-send when available. If it isn't installed
// we silently no-op — notifications are best-effort and we don't want
// to error-spam the GUI on minimal Linux desktops. The tag is used as
// the replace-id so repeat bells from the same session collapse.
func platformNotify(title, subtitle, body, tag string) error {
	path, err := exec.LookPath("notify-send")
	if err != nil {
		return nil
	}
	// notify-send doesn't expose subtitle, so fold it into the body.
	combined := body
	if subtitle != "" {
		if combined != "" {
			combined = subtitle + " — " + combined
		} else {
			combined = subtitle
		}
	}
	args := []string{"--app-name=Hive"}
	if tag != "" {
		args = append(args, "--hint=string:x-canonical-private-synchronous:"+tag)
	}
	args = append(args, title, combined)
	return exec.Command(path, args...).Run()
}
