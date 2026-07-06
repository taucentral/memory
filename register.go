// register.go — the Plugin constructor and middleware accessors.
//
// One Plugin instance is registered as BOTH a tau.RequestMutator
// (retrieve) and a tau.ResponseObserver (persist) via two distinct
// objects sharing the same Config. The embedder adds both to
// Options.Middleware; the runtime's partitionMiddleware routes each
// to the appropriate slice.
//
// Usage:
//
//	plugin := memory.New(memory.Config{Store: store, Scope: "project"})
//	opts.Middleware = append(opts.Middleware, plugin.Mutator(), plugin.Observer())
package memory

import (
	tau "github.com/taucentral/tau/pkg/tau"
)

// Plugin bundles a retrieve mutator and a persist observer sharing
// the same Config. Construct via New.
type Plugin struct {
	cfg      Config
	mutator  *memRetrieveMutator
	observer *memPersistObserver
}

// New constructs a Plugin from cfg, applying defaults to zero-value
// fields. The returned Plugin exposes one mutator and one observer;
// both share the same Config copy so retrieve and persist use
// symmetric scope tags and ID conventions.
func New(cfg Config) *Plugin {
	cfg.applyDefaults()
	mut := &memRetrieveMutator{cfg: cfg}
	obs := &memPersistObserver{cfg: cfg}
	return &Plugin{cfg: cfg, mutator: mut, observer: obs}
}

// Mutator returns the retrieve mutator. Add the result to
// Options.Middleware to enable memory injection into req.System.
func (p *Plugin) Mutator() tau.RequestMutator { return p.mutator }

// Observer returns the persist observer. Add the result to
// Options.Middleware to enable memory extraction from responses.
func (p *Plugin) Observer() tau.ResponseObserver { return p.observer }

// Compile-time assertions: the plugin's middleware types satisfy the
// public SDK interfaces. These catch interface drift at build time.
var (
	_ tau.RequestMutator    = (*memRetrieveMutator)(nil)
	_ tau.ResponseObserver  = (*memPersistObserver)(nil)
)
