// Package alert is a thin wrapper around the beeep package. This entire package is
// best effort and should not be heavily relied on (it is only for a better UX).
package alert

import "github.com/gen2brain/beeep"

// alertTitle is the title given to all alerts (see the Alert function).
const alertTitle = "Kubernetes Developer Environment"

// Alert is a best effort function that hooks into the operating system's alerting
// mechanism to send the user a message.
func Alert(message string) {
	_ = beeep.Alert(alertTitle, message, "") //nolint:errcheck // Why: This is best effort, and may not be supported on all platforms. 
	_ = beeep.Beep(beeep.DefaultFreq, 2000) //nolint:errcheck // Why: This is best effort, and may not be supported on all platforms.
}
