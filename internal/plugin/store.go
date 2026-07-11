package plugin

import (
	"context"

	"github.com/0x0abc123/byteswarm/internal/consumer"
)

// NamespacedStore adapts a consumer.Repository into the script `store`
// capability, confining every key to a host-controlled namespace derived from
// the plugin name (ADR-0008). Keys are prefixed host-side so one plugin cannot
// address another's data; the Repository port signature is unchanged —
// namespacing lives here in the shim, not in a new port argument.
type NamespacedStore struct {
	repo      consumer.Repository
	namespace string
}

// NewNamespacedStore binds a repository to a plugin's namespace.
func NewNamespacedStore(repo consumer.Repository, namespace string) *NamespacedStore {
	return &NamespacedStore{repo: repo, namespace: namespace}
}

// key prefixes a script-supplied key with the plugin's namespace. The
// separator is host-controlled and the namespace is not script-supplied, so a
// script cannot forge a prefix to reach another namespace.
func (s *NamespacedStore) key(k string) string {
	return s.namespace + ":" + k
}

// Load reads namespaced state.
func (s *NamespacedStore) Load(ctx context.Context, id string) ([]byte, error) {
	return s.repo.Load(ctx, s.key(id))
}

// Save writes namespaced state.
func (s *NamespacedStore) Save(ctx context.Context, id string, state []byte) error {
	return s.repo.Save(ctx, s.key(id), state)
}
