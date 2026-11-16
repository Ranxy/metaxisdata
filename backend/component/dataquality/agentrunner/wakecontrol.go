package agentrunner

import "time"

// wakeControl is the state one wake's control tools may change. The engine owns it
// and applies it once after the tool batch, so a tool returns a value instead of
// mutating five out-parameters through pointers.
type wakeControl struct {
	blocked        bool
	blockedCode    string
	blockedWhy     string
	nextWake       *time.Time
	scheduleReason string
}
