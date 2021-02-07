package cache

import (
	"fmt"
	"time"

	memdb "github.com/hashicorp/go-memdb"
	"k8s.io/client-go/tools/cache"
)

type cacheKnownType struct {
	Key string
	Obj interface{}
}

type MemdbIndexerKeyFunc cache.KeyFunc

func (fn MemdbIndexerKeyFunc) FromArgs(args ...interface{}) ([]byte, error) {
	if len(args) > 1 || len(args) == 0 {
		return nil, fmt.Errorf("must provide one argument")
	}
	key, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("argument must be a string: %#v", args[0])
	}
	return []byte(key), nil
}

func (fn MemdbIndexerKeyFunc) FromObject(raw interface{}) (bool, []byte, error) {
	switch t := raw.(type) {
	case cache.DeletedFinalStateUnknown:
		if len(t.Key) > 0 {
			return true, []byte(t.Key), nil
		}
		s, err := fn(t.Obj)
		if err != nil {
			return false, nil, err
		}
		t.Key = s
		return true, []byte(s), nil
	case cacheKnownType:
		if len(t.Key) > 0 {
			return true, []byte(t.Key), nil
		}
		s, err := fn(t.Obj)
		if err != nil {
			return false, nil, err
		}
		t.Key = s
		return true, []byte(s), nil
	}
	return false, nil, fmt.Errorf("unrecognized key index type: %T", raw)
}

func NewMemdbStore() (cache.Indexer, error) {
	objectTable := &memdb.TableSchema{
		Name: "object",
		Indexes: map[string]*memdb.IndexSchema{
			"id": {
				Name:    "id",
				Unique:  true,
				Indexer: MemdbIndexerKeyFunc(cache.DeletionHandlingMetaNamespaceKeyFunc),
			},
		},
	}
	if err := objectTable.Validate(); err != nil {
		return nil, fmt.Errorf("object table is invalid: %v", err)
	}
	db, err := memdb.NewMemDB(&memdb.DBSchema{
		Tables: map[string]*memdb.TableSchema{
			"object": objectTable,
		},
	})
	if err != nil {
		return nil, err
	}
	return &memdbStore{
		keyFn: cache.DeletionHandlingMetaNamespaceKeyFunc,
		obj:   objectTable,
		db:    db,
	}, nil
}

type memdbStore struct {
	keyFn cache.KeyFunc
	obj   *memdb.TableSchema
	db    *memdb.MemDB
}

// Add adds the given object to the accumulator associated with the given object's key
func (i *memdbStore) Add(obj interface{}) error {
	txn := i.db.Txn(true)
	if err := txn.Insert(i.obj.Name, cacheKnownType{Obj: obj}); err != nil {
		return err
	}
	txn.Commit()
	return nil
}

// Update updates the given object in the accumulator associated with the given object's key
func (i *memdbStore) Update(obj interface{}) error {
	panic("not implemented") // TODO: Implement
}

// Delete deletes the given object from the accumulator associated with the given object's key
func (i *memdbStore) Delete(obj interface{}) error {
	panic("not implemented") // TODO: Implement
}

// List returns a list of all the currently non-empty accumulators
func (i *memdbStore) List() []interface{} {
	panic("not implemented") // TODO: Implement
}

// ListKeys returns a list of all the keys currently associated with non-empty accumulators
func (i *memdbStore) ListKeys() []string {
	txn := i.db.Txn(false)
	it, err := txn.Get(i.obj.Name, "id")
	if err != nil {
		panic(err)
	}
	var keys []string
	for next := it.Next(); next != nil; next = it.Next() {
		switch t := next.(type) {
		case cacheKnownType:
			keys = append(keys, t.Key)
		}
	}
	return keys
}

// Get returns the accumulator associated with the given object's key
func (i *memdbStore) Get(obj interface{}) (item interface{}, exists bool, err error) {
	txn := i.db.Txn(false)
	key, err := i.keyFn(obj)
	if err != nil {
		return nil, false, err
	}
	out, err := txn.First(i.obj.Name, "id", key)
	if err != nil {
		return nil, false, err
	}
	switch t := out.(type) {
	case cache.DeletedFinalStateUnknown:
		return nil, false, nil
	case cacheKnownType:
		return t.Obj, true, nil
	}
	return nil, false, nil
}

// GetByKey returns the accumulator associated with the given key
func (i *memdbStore) GetByKey(key string) (item interface{}, exists bool, err error) {
	panic("not implemented") // TODO: Implement
}

// Replace will delete the contents of the store, using instead the
// given list. Store takes ownership of the list, you should not reference
// it after calling this function.
func (i *memdbStore) Replace(_ []interface{}, _ string) error {
	panic("not implemented") // TODO: Implement
}

// Resync is meaningless in the terms appearing here but has
// meaning in some implementations that have non-trivial
// additional behavior (e.g., DeltaFIFO).
func (i *memdbStore) Resync() error {
	panic("not implemented") // TODO: Implement
}

// Index returns the stored objects whose set of indexed values
// intersects the set of indexed values of the given object, for
// the named index
func (i *memdbStore) Index(indexName string, obj interface{}) ([]interface{}, error) {
	panic("not implemented") // TODO: Implement
}

// IndexKeys returns the storage keys of the stored objects whose
// set of indexed values for the named index includes the given
// indexed value
func (i *memdbStore) IndexKeys(indexName string, indexedValue string) ([]string, error) {
	panic("not implemented") // TODO: Implement
}

// ListIndexFuncValues returns all the indexed values of the given index
func (i *memdbStore) ListIndexFuncValues(indexName string) []string {
	panic("not implemented") // TODO: Implement
}

// ByIndex returns the stored objects whose set of indexed values
// for the named index includes the given indexed value
func (i *memdbStore) ByIndex(indexName string, indexedValue string) ([]interface{}, error) {
	panic("not implemented") // TODO: Implement
}

// GetIndexer return the indexers
func (i *memdbStore) GetIndexers() cache.Indexers {
	panic("not implemented") // TODO: Implement
}

// AddIndexers adds more indexers to this store.  If you call this after you already have data
// in the store, the results are undefined.
func (i *memdbStore) AddIndexers(newIndexers cache.Indexers) error {
	panic("not implemented") // TODO: Implement
}

type memdbInformer struct {
	memdbStore
}

func NewMemdbInformer() (cache.SharedIndexInformer, error) {
	objectTable := &memdb.TableSchema{
		Name: "object",
		Indexes: map[string]*memdb.IndexSchema{
			"id": {
				Name:    "id",
				Unique:  true,
				Indexer: MemdbIndexerKeyFunc(cache.DeletionHandlingMetaNamespaceKeyFunc),
			},
		},
	}
	if err := objectTable.Validate(); err != nil {
		return nil, fmt.Errorf("object table is invalid: %v", err)
	}
	db, err := memdb.NewMemDB(&memdb.DBSchema{
		Tables: map[string]*memdb.TableSchema{
			"object": objectTable,
		},
	})
	if err != nil {
		return nil, err
	}
	return &memdbInformer{
		memdbStore: memdbStore{
			keyFn: cache.DeletionHandlingMetaNamespaceKeyFunc,
			obj:   objectTable,
			db:    db,
		},
	}, nil
}

// AddEventHandler adds an event handler to the shared informer using the shared informer's resync
// period.  Events to a single handler are delivered sequentially, but there is no coordination
// between different handlers.
func (i *memdbInformer) AddEventHandler(handler cache.ResourceEventHandler) {
	panic("not implemented") // TODO: Implement
}

// AddEventHandlerWithResyncPeriod adds an event handler to the
// shared informer with the requested resync period; zero means
// this handler does not care about resyncs.  The resync operation
// consists of delivering to the handler an update notification
// for every object in the informer's local cache; it does not add
// any interactions with the authoritative storage.  Some
// informers do no resyncs at all, not even for handlers added
// with a non-zero resyncPeriod.  For an informer that does
// resyncs, and for each handler that requests resyncs, that
// informer develops a nominal resync period that is no shorter
// than the requested period but may be longer.  The actual time
// between any two resyncs may be longer than the nominal period
// because the implementation takes time to do work and there may
// be competing load and scheduling noise.
func (i *memdbInformer) AddEventHandlerWithResyncPeriod(handler cache.ResourceEventHandler, resyncPeriod time.Duration) {
	panic("not implemented") // TODO: Implement
}

// GetStore returns the informer's local cache as a Store.
func (i *memdbInformer) GetStore() cache.Store {
	return i
}

// GetController is deprecated, it does nothing useful
func (i *memdbInformer) GetController() cache.Controller {
	return i
}

// Run starts and runs the shared informer, returning after it stops.
// The informer will be stopped when stopCh is closed.
func (i *memdbInformer) Run(stopCh <-chan struct{}) {
	panic("not implemented") // TODO: Implement
}

// HasSynced returns true if the shared informer's store has been
// informed by at least one full LIST of the authoritative state
// of the informer's object collection.  This is unrelated to "resync".
func (i *memdbInformer) HasSynced() bool {
	panic("not implemented") // TODO: Implement
}

// LastSyncResourceVersion is the resource version observed when last synced with the underlying
// store. The value returned is not synchronized with access to the underlying store and is not
// thread-safe.
func (i *memdbInformer) LastSyncResourceVersion() string {
	panic("not implemented") // TODO: Implement
}

// The WatchErrorHandler is called whenever ListAndWatch drops the
// connection with an error. After calling this handler, the informer
// will backoff and retry.
//
// The default implementation looks at the error type and tries to log
// the error message at an appropriate level.
//
// There's only one handler, so if you call this multiple times, last one
// wins; calling after the informer has been started returns an error.
//
// The handler is intended for visibility, not to e.g. pause the consumers.
// The handler should return quickly - any expensive processing should be
// offloaded.
func (i *memdbInformer) SetWatchErrorHandler(handler cache.WatchErrorHandler) error {
	panic("not implemented") // TODO: Implement
}

// AddIndexers add indexers to the informer before it starts.
func (i *memdbInformer) AddIndexers(indexers cache.Indexers) error {
	panic("not implemented") // TODO: Implement
}

func (i *memdbInformer) GetIndexer() cache.Indexer {
	panic("not implemented") // TODO: Implement
}
