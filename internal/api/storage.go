package api

import (
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/nil2x/cheburnet/internal/datagram"
	"github.com/nil2x/cheburnet/internal/transform"
)

type storageNamespace int

const (
	storageNamespaceUnknown storageNamespace = iota
	storageNamespaceA
	storageNamespaceB
)

// StorageClient implements virtual state of storage. Intended to work together
// with VKClient. See https://dev.vk.com/ru/method/storage.
//
// You should create one StorageClient per program. Until call of UpdateNamespace
// with Datagram value, write collision is possible.
//
// The problem is that you have more than one instance of the program that perform
// i/o operations over the same storage. The storage and programs have no synchronization logic.
// Storage, on its side, forces limit of total 1000 keys. Thus, risk of collision exists,
// when multiple programs trying to perform i/o operation over the same key at the same time.
// Such collision will not be catched, leading to hidden bugs.
//
// StorageClient resolves this problem by implementing synchronization logic in the code.
// All programs must use StorageClient and write Datagram values, otherwise this synchronization
// logic will not work properly.
type StorageClient struct {
	mu                 sync.Mutex
	namespace          storageNamespace
	namespaceChangedAt time.Time
	nextKey            int
}

func NewStorageClient() *StorageClient {
	return &StorageClient{
		mu:                 sync.Mutex{},
		namespace:          storageNamespaceUnknown,
		namespaceChangedAt: time.Time{},
		nextKey:            0,
	}
}

// UpdateNamespace should be called on every value after read from storage.
// It is expected that most of storage values are Datagram values.
// After UpdateNamespace call with Datagram value, CreateSetKey will produce
// keys that have no risk of collision.
func (c *StorageClient) UpdateNamespace(value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// No need to update often.
	if time.Since(c.namespaceChangedAt) < 10*time.Second {
		return
	}

	// Only Datagram values can be used.
	if transform.IsTextURL(value) {
		return
	}

	dg, err := datagram.Decode(value)

	// Only valid Datagram values of another program instance can be used.
	if err != nil || dg.IsLoopback() {
		return
	}

	me := datagram.DeviceID
	you := dg.Device
	newNS := storageNamespaceUnknown

	// One branch will fire on "me" instance side, another branch will fire on "you" instance side.
	if me < you {
		newNS = storageNamespaceA
	} else if me > you {
		newNS = storageNamespaceB
	}

	if newNS != c.namespace {
		slog.Debug("api: storage namespace change", "old", c.namespace, "new", newNS)
	}

	c.namespace = newNS
	c.namespaceChangedAt = time.Now()
}

// CreateGetKeys creates keys that should be used for VKClient.StorageGet operation.
// You must not use your own keys.
func (c *StorageClient) CreateGetKeys() []string {
	keys := []string{}

	// Although the limit is 1000, 200 keys are enough for the program.
	for i := 1; i <= 200; i++ {
		keys = append(keys, fmt.Sprintf("key-%v", i))
	}

	return keys
}

// CreateSetKey creates key that should be used for VKClient.StorageSet operation.
// You must not use your own key.
func (c *StorageClient) CreateSetKey() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := 0

	if c.namespace == storageNamespaceUnknown {
		// Probability of collision is 1/200.
		key = rand.Intn(200) + 1
	} else {
		if c.namespace == storageNamespaceA && (c.nextKey < 1 || c.nextKey > 100) {
			c.nextKey = 1
		} else if c.namespace == storageNamespaceB && (c.nextKey < 101 || c.nextKey > 200) {
			c.nextKey = 101
		}

		key = c.nextKey
		c.nextKey++
	}

	return fmt.Sprintf("key-%v", key)
}

// DiffValues takes two StorageGetResponse and returns difference between them.
//
// oldValues are supposed to be previous response of VKClient.StorageGet operation,
// newValues are supposed to be current response of VKClient.StorageGet operation.
// A difference between them are values that are new or changed their value.
//
// You should keep the track of oldValues.
func (c *StorageClient) DiffValues(oldValues, newValues []StorageGetResponse) []StorageGetResponse {
	if len(oldValues) == 0 {
		return newValues
	}

	changed := []StorageGetResponse{}
	oldMap := map[string]string{}

	for _, v := range oldValues {
		oldMap[v.Key] = v.Value
	}

	for _, v := range newValues {
		if oldValue, exists := oldMap[v.Key]; !exists || oldValue != v.Value {
			changed = append(changed, v)
		}
	}

	return changed
}
