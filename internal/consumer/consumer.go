// Package consumer holds the consumer registry and the compile-time plugin SDK
// surface (ADR-0001, ADR-0003), and declares the Repository port consumers use
// to persist state. The store adapter implements Repository; per ADR-0005 the
// production adapter is PostgreSQL and the embedded adapter is SQLite, both on
// database/sql behind this port.
package consumer

import "context"

// Repository is the outbound persistence port for a consumer aggregate's
// state. Kept per-aggregate (ADR-0005) so consumers stay persistence-agnostic
// and the SQLite<->PostgreSQL choice is a composition-root decision.
type Repository interface {
	Load(ctx context.Context, id string) ([]byte, error)
	Save(ctx context.Context, id string, state []byte) error
}
