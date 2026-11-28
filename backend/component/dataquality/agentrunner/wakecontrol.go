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
	// wake is the wake number, for the adoption audit correlation.
	wake int32
	// ownerAsked reports that this wake is answering a person's question. The
	// adoption tool is exposed only then: a scheduled or event wake must never
	// adopt on its own.
	ownerAsked bool
	// adoptionUsed reports that the adoption tool ran, so the automatic pass is
	// suppressed for this wake.
	adoptionUsed bool
	// ownerFacing reports that this wake wrote or may write owner-visible prose.
	ownerFacing bool
	// ownerMessageWritten reports that ask_owner or reply_owner already wrote the
	// OWNER row, so the final assistant text must stay INTERNAL.
	ownerMessageWritten bool
}
