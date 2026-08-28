package notify

import toast "git.sr.ht/~jackmordaunt/go-toast/v2"

func platformNotify(title, subtitle, body, tag string) error {
	combined := body
	if subtitle != "" {
		if combined != "" {
			combined = subtitle + " — " + combined
		} else {
			combined = subtitle
		}
	}
	n := toast.Notification{
		AppID: "Hive",
		Title: title,
		Body:  combined,
	}
	return n.Push()
}

// setActivationHandler is a no-op: Windows notifications carry no
// click-back channel, so the handler would never be called and holding
// it would only keep the closure alive. See SetActivationHandler.
func setActivationHandler(func(tag string)) {}
