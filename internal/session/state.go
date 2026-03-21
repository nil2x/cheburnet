package session

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/nil2x/cheburnet/internal/api"
	"github.com/nil2x/cheburnet/internal/config"
	"github.com/nil2x/cheburnet/internal/datagram"
)

// Init must be called at the program start before Session usage.
func Init(cfg config.Config, vkC *api.VKClient, storageC *api.StorageClient) error {
	initPlanner(cfg)
	initMultiplexer(cfg, vkC, storageC)

	return nil
}

var sessions = map[datagram.Ses]*Session{}
var sessionsMu = sync.Mutex{}

// Get returns session from the global state.
func Get(id datagram.Ses) (*Session, bool) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	ses, exists := sessions[id]

	return ses, exists
}

// Set sets session in the global state.
func Set(id datagram.Ses, ses *Session) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	sessions[id] = ses
}

// IsOpened returns if at least one session from the global state is in opened state.
func IsOpened() bool {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	for _, ses := range sessions {
		if !ses.IsClosed() {
			return true
		}
	}

	return false
}

var sessionID = datagram.Ses(0)
var sessionIDMu = sync.Mutex{}

// NextID returns next session id that should be used in Open.
// For sessions that arrive you should use arrived session id.
func NextID() datagram.Ses {
	sessionIDMu.Lock()
	defer sessionIDMu.Unlock()

	sessionID++

	return sessionID
}

// Clear periodically clears the global state from closed or inactive sessions.
func Clear(ctx context.Context) error {
	timeoutInterval := 10 * time.Second
	deleteInterval := 2 * time.Minute

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(timeoutInterval):
				sessionsMu.Lock()

				for id, ses := range sessions {
					if ses.IsInactive() {
						slog.Error("session: timeout", "id", id)

						go func(ses *Session) {
							ses.Close()
						}(ses)
					}
				}

				sessionsMu.Unlock()
			}
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(deleteInterval):
				sessionsMu.Lock()

				for id, ses := range sessions {
					if ses.IsClosed() {
						delete(sessions, id)
					}
				}

				sessionsMu.Unlock()
			}
		}
	}()

	wg.Wait()

	return nil
}
