package session

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/nil2x/cheburnet/internal/api"
	"github.com/nil2x/cheburnet/internal/config"
	"github.com/nil2x/cheburnet/internal/datagram"
)

var (
	errSessionClosed     = errors.New("session is closed")
	errSessionBufferFull = errors.New("session buffer is full")
	errSessionPeerNil    = errors.New("session peer is nil")
	errSessionMismatch   = errors.New("session mismatch")
)

type ConnWriteFunc func(net.Conn, []byte) error

// Session represents virtual connection between a local peer and a remote peer.
//
// Local peer is managed by one program instance, remote peer is managed by
// another program instance. Both programs run on a different computers in a
// different networks and unaware of each other. There is no any OS level
// synchronization logic between two peers. Session links two peers together.
// It decides how to send data to the remote peer and tracks connection state.
//
// Session is intended to be used with Datagram. The program should synchronize
// Session.ID on both ends, i.e. if program A opens session with id 1 and sends
// first datagram, program B upon receiving this datagram also should open session
// with id 1 and response with a new datagram over this session.
type Session struct {
	ID           datagram.Ses
	OnClose      chan struct{}
	cfg          config.Config
	mu           sync.Mutex
	wg           sync.WaitGroup
	local        net.Conn
	localWrite   ConnWriteFunc
	planner      *planner
	executor     executorI
	toLocal      chan []byte
	toRemote     chan datagram.Datagram
	isClosed     bool
	number       datagram.Num
	history      map[datagram.Num]datagram.Datagram
	openedAt     time.Time
	closedAt     time.Time
	activeAt     time.Time
	forwardedIn  int
	forwardedOut int
}

// Open opens a new session. It must be closed upon finishing local or remote peer connection.
//
// id must be synchronized by all program instances. Data to the remote peer will be sent
// using provided API clients.
func Open(cfg config.Config, vkC *api.VKClient, storageC *api.StorageClient, id datagram.Ses) (*Session, error) {
	slog.Debug("session: open", "id", id)

	now := time.Now()
	s := &Session{
		ID:           id,
		OnClose:      make(chan struct{}),
		cfg:          cfg,
		mu:           sync.Mutex{},
		wg:           sync.WaitGroup{},
		local:        nil,
		localWrite:   nil,
		planner:      nil,
		executor:     nil,
		toLocal:      make(chan []byte, 100),
		toRemote:     make(chan datagram.Datagram, 100),
		isClosed:     false,
		number:       0,
		history:      make(map[datagram.Num]datagram.Datagram),
		openedAt:     now,
		closedAt:     time.Time{},
		activeAt:     now,
		forwardedIn:  0,
		forwardedOut: 0,
	}

	if muxer == nil {
		s.executor = newExecutor(cfg, vkC, storageC, id)
	} else {
		s.executor = muxer
	}

	s.planner = newPlanner(cfg, s, s.executor)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.listenToLocal()
	}()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.listenToRemote()
	}()

	return s, nil
}

// Close closes the session and makes it unusable for further write operations.
// If local peer was set, it also will be closed.
//
// Close waits completion of all pending write operations, so it may take some
// time to complete.
//
// After completion, OnClose channel will be closed.
func (s *Session) Close() {
	s.mu.Lock()

	if s.isClosed {
		s.mu.Unlock()
		return
	}

	if s.local == nil {
		slog.Debug("session: close", "id", s.ID)
	} else {
		slog.Debug("session: close", "id", s.ID, "peer", s.local.RemoteAddr().String())
	}

	slog.Debug(
		"session: stats",
		"id", s.ID,
		"in", s.forwardedIn,
		"out", s.forwardedOut,
		"duration", int(time.Since(s.openedAt).Seconds()),
		"fragments", len(s.history),
	)

	s.isClosed = true

	close(s.toLocal)
	close(s.toRemote)

	s.mu.Unlock()

	s.wg.Wait()

	if s.executor != muxer {
		s.executor.wait()
	}

	s.mu.Lock()

	if s.local != nil {
		s.local.Close()
	}

	close(s.OnClose)
	s.closedAt = time.Now()

	s.mu.Unlock()
}

// String returns short representation for debugging purposes.
func (s *Session) String() string {
	return fmt.Sprint(s.ID)
}

// IsClosed returns if Close was called.
func (s *Session) IsClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.isClosed
}

// SinceClose returns duration since Close is completed.
// If Close wasn't called or not yet completed, zero is returned.
func (s *Session) SinceClose() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closedAt.IsZero() {
		return 0
	}

	return time.Since(s.closedAt)
}

// IsInactive returns if there was no any write operations for the configured
// amount of time.
//
// If Close was called, false will be returned.
func (s *Session) IsInactive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isClosed {
		return false
	}

	timeout := s.cfg.Session.Timeout()

	if timeout == 0 {
		return false
	}

	return time.Since(s.activeAt) > timeout
}

// GetHistory returns datagram that was previously sent to the remote peer.
//
// Second value indicates if requested datagram is missing and never was produced
// by the session.
//
// GetHistory may return different datagrams than were sent using WriteRemote.
func (s *Session) GetHistory(num datagram.Num) (datagram.Datagram, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dg, exists := s.history[num]

	return dg, exists
}

// SetLocal sets a local peer the session should be associated with.
// Typically it is called only once during session lifetime.
//
// write defines function to write bytes data to the local peer.
//
// If SetLocal was called, Close will close this connection, so you don't
// need to close it manually.
func (s *Session) SetLocal(conn net.Conn, write ConnWriteFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.local = conn
	s.localWrite = write
}

// WriteLocal writes bytes data to the local peer that was set using SetLocal.
// It is non-blocking function.
func (s *Session) WriteLocal(b []byte) error {
	clone := bytes.Clone(b)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isClosed {
		return errSessionClosed
	}

	if s.local == nil || s.localWrite == nil {
		return errSessionPeerNil
	}

	s.activeAt = time.Now()
	s.forwardedIn += len(clone)

	select {
	case s.toLocal <- clone:
		return nil
	default:
		return errSessionBufferFull
	}
}

// WriteRemote sends datagram to the remote peer on another program end.
// It is non-blocking function.
//
// dg.Session and dg.Number can be zero, in that case they will be set automatically.
// It is recommended for dg.Number to be zero, this will allow to split it into smaller
// datagrams for more efficient sending.
//
// If you choose to specify zero value for dg.Number, you should do it in all WriteRemote calls.
// You must not mix specifying zero and non-zero values. The only exception is datagrams from
// GetHistory, they are allowed to be sent again.
//
// Because dg may be splitted, GetHistory will return datagrams that actually
// were sent, not the ones that were passed into WriteRemote.
func (s *Session) WriteRemote(dg datagram.Datagram) error {
	clone := dg.Clone()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isClosed {
		return errSessionClosed
	}

	if clone.Session == 0 {
		clone.Session = s.ID
	}

	if clone.Session != s.ID {
		return errSessionMismatch
	}

	s.activeAt = time.Now()

	if dg.Command == datagram.CommandForward {
		s.forwardedOut += len(clone.Payload)
	}

	select {
	case s.toRemote <- clone:
		return nil
	default:
		return errSessionBufferFull
	}
}

// listenToLocal listens WriteLocal.
func (s *Session) listenToLocal() {
	for b := range s.toLocal {
		if err := s.localWrite(s.local, b); err != nil {
			slog.Error("session: local", "id", s.ID, "err", err)
		}
	}
}

// listenToRemote listens WriteRemote.
func (s *Session) listenToRemote() {
	for dg := range s.toRemote {
		plan, err := s.planner.create(dg)

		if err != nil {
			slog.Error("session: remote", "id", s.ID, "dg", dg, "err", err)
			continue
		}

		s.mu.Lock()

		for _, fg := range plan.fragments {
			s.history[fg.Number] = fg
		}

		s.mu.Unlock()

		if err := s.executor.execute(plan); err != nil {
			slog.Error("session: remote", "id", s.ID, "dg", dg, "err", err)
			continue
		}
	}
}

// nextNumber tracks datagram ordering and returns next number that should be
// assigned to a new datagram.
func (s *Session) nextNumber() datagram.Num {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.number++

	return s.number
}
