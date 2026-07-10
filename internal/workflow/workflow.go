// Package workflow owns workflow lifecycle and resilience concerns —
// subscription, reconnect, and redelivery (ADR-0001, ADR-0004). It is a domain
// package: it holds lifecycle state and rules, and depends only on ports
// declared by the domain, never on adapters.
package workflow

// State is the lifecycle state of a workflow subscription.
type State int

const (
	// StatePending is the initial state before the subscription is established.
	StatePending State = iota
	// StateRunning means the subscription is active and delivering events.
	StateRunning
	// StateStopped means the subscription has been torn down.
	StateStopped
)

// String renders the state for logs and operator output.
func (s State) String() string {
	switch s {
	case StatePending:
		return "pending"
	case StateRunning:
		return "running"
	case StateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}
