package schemasync

import (
	"sync"

	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// databaseSyncGate makes one schema sync per database run at a time inside the
// process. Two concurrent syncs would each diff the digest they read before
// either writes, so the later commit can delete a row the other just created or
// deadlock against it. A caller that arrives while a sync is running waits for
// it and reuses its outcome instead of starting a second sync.
type databaseSyncGate struct {
	mu       sync.Mutex
	inFlight map[string]*databaseSyncCall
}

// databaseSyncCall is one database's running sync: done is closed when the
// owner published err.
type databaseSyncCall struct {
	done chan struct{}
	err  error
}

// acquire returns the call that owns the sync for key, or the call to wait on
// when another caller already owns it.
func (g *databaseSyncGate) acquire(key string) (call *databaseSyncCall, owner bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight == nil {
		g.inFlight = map[string]*databaseSyncCall{}
	}
	if existing, ok := g.inFlight[key]; ok {
		return existing, false
	}
	call = &databaseSyncCall{done: make(chan struct{})}
	g.inFlight[key] = call
	return call, true
}

// owns reports whether some caller currently holds this database's sync. A
// database being synced has already been dequeued, so it is invisible to the queue
// and only this knows it is still in progress.
func (g *databaseSyncGate) owns(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.inFlight[key]
	return ok
}

// finish publishes the owner's result and releases the waiters. The entry is
// removed before done is closed, so a caller arriving after this point starts a
// fresh sync instead of reusing a finished one.
func (g *databaseSyncGate) finish(key string, call *databaseSyncCall, err error) {
	g.mu.Lock()
	call.err = err
	delete(g.inFlight, key)
	g.mu.Unlock()
	close(call.done)
}

// instanceSyncGate makes one instance enumeration run at a time inside the
// process. Two concurrent enumerations would each read the database list, create
// the same missing rows and write the same soft deletes, so the deleted flags and
// LastSyncTime would depend on commit order rather than on the target's schema.
type instanceSyncGate struct {
	mu       sync.Mutex
	inFlight map[string]*instanceSyncCall
}

// instanceSyncCall is one instance's running enumeration: done is closed when the
// owner published its result.
type instanceSyncCall struct {
	done            chan struct{}
	updatedInstance *store.InstanceMessage
	databases       []*storepb.DatabaseSchemaMetadata
	newDatabases    []*store.DatabaseMessage
	err             error
}

func (g *instanceSyncGate) acquire(key string) (call *instanceSyncCall, owner bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight == nil {
		g.inFlight = map[string]*instanceSyncCall{}
	}
	if existing, ok := g.inFlight[key]; ok {
		return existing, false
	}
	call = &instanceSyncCall{done: make(chan struct{})}
	g.inFlight[key] = call
	return call, true
}
func (g *instanceSyncGate) finish(key string, call *instanceSyncCall, updated *store.InstanceMessage, databases []*storepb.DatabaseSchemaMetadata, newDatabases []*store.DatabaseMessage, err error) {
	g.mu.Lock()
	call.updatedInstance = updated
	call.databases = databases
	call.newDatabases = newDatabases
	call.err = err
	delete(g.inFlight, key)
	g.mu.Unlock()
	close(call.done)
}
