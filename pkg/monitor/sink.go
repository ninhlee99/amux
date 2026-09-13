package monitor

import "amux-accounts/pkg/term"

// EnableTermSink wires term.Log → events.log so realtime proxy/CLI
// events are persisted for `amux logs` and monitoring.
func EnableTermSink() {
	term.SetEventSink(func(tag, msg string) {
		AppendEvent(tag, msg)
	})
}
