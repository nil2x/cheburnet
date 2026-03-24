package handler

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nil2x/cheburnet/internal/config"
	"github.com/nil2x/cheburnet/internal/datagram"
	"github.com/nil2x/cheburnet/internal/session"
)

// reassemblyBuffer accepts datagrams of a single session in any order and
// executes their processing in correct order.
//
// If datagram that is required to maintain the order is missing, it will be
// waited for and all further but already received datagrams will be on hold.
// If it takes too long for the missing datagram to arrive, retry command will be
// issued to the remote side. If after multiple retries the missing datagram still
// not here, the session is closed.
//
// After session close the buffer is automatically closed.
type reassemblyBuffer struct {
	cfg      config.Config
	ses      *session.Session
	mu       sync.Mutex
	closed   bool
	closedAt time.Time
	temp     []datagram.Datagram
	data     map[datagram.Num]datagram.Datagram
	next     datagram.Num
	pending  datagram.Num
	retries  int
	signal   chan struct{}
}

func openReassemblyBuffer(cfg config.Config, ses *session.Session) *reassemblyBuffer {
	slog.Debug("handler: open", "ses", ses)

	rb := &reassemblyBuffer{
		cfg:      cfg,
		ses:      ses,
		mu:       sync.Mutex{},
		closed:   false,
		closedAt: time.Time{},
		temp:     []datagram.Datagram{},
		data:     map[datagram.Num]datagram.Datagram{},
		next:     1,
		pending:  0,
		retries:  0,
		signal:   make(chan struct{}, 1),
	}

	go func() {
		rb.listen()
		rb.close()
	}()

	return rb
}

func (rb *reassemblyBuffer) close() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.closed {
		return
	}

	slog.Debug("handler: close", "ses", rb.ses)

	clear(rb.temp)
	clear(rb.data)

	rb.closed = true
	rb.closedAt = time.Now()
}

func (rb *reassemblyBuffer) isClosed() bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	return rb.closed
}

func (rb *reassemblyBuffer) sinceClose() time.Duration {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.closedAt.IsZero() {
		return 0
	}

	return time.Since(rb.closedAt)
}

func (rb *reassemblyBuffer) push(dg datagram.Datagram) error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if dg.Session != rb.ses.ID {
		return errors.New("buffer mismatch")
	}

	if rb.closed {
		// At this stage session already closed. Safe to ignore it.
		if dg.Command == datagram.CommandClose {
			return nil
		}

		// Most likely it is TLS close_notify alert. In TLS 1.3 peer A may send
		// close_notify and immediately close the connection without waiting for
		// peer B close_notify response. Let's also ignore it, to not flood the log.
		if (dg.Command == datagram.CommandForward) && (len(dg.Payload) == 24) {
			return nil
		}

		// Peer A finished and closed, but peer B didn't receive some of peer A data.
		// In this case, peer A should send its history even if it is closed.
		if dg.Command == datagram.CommandRetry {
			go func() {
				if err := handleCommand(rb.cfg, rb.ses, dg); err != nil {
					slog.Error("handler: command", "dg", dg, "err", err)
				}
			}()

			return nil
		}

		return errors.New("buffer is closed")
	}

	rb.temp = append(rb.temp, dg)

	select {
	case rb.signal <- struct{}{}:
	default:
	}

	return nil
}

func (rb *reassemblyBuffer) listen() {
	retryInterval := rb.cfg.Handler.RetryInterval()

	for {
		var retryCh <-chan time.Time

		if retryInterval > 0 {
			retryCh = time.After(retryInterval)
		}

		stop := false

		select {
		case <-rb.signal:
			stop = rb.handle()
		case <-retryCh:
			stop = rb.retry()
		case <-rb.ses.OnClose:
			return
		}

		if stop {
			rb.send(datagram.CommandClose, nil)
			handleClose(rb.ses)
			return
		}
	}
}

func (rb *reassemblyBuffer) handle() bool {
	rb.mu.Lock()

	for _, dg := range rb.temp {
		rb.data[dg.Number] = dg
	}

	rb.temp = []datagram.Datagram{}

	rb.mu.Unlock()

	for {
		dg, exists := rb.data[rb.next]

		if !exists {
			break
		}

		if err := handleCommand(rb.cfg, rb.ses, dg); err != nil {
			slog.Error("handler: command", "dg", dg, "err", err)
			return true
		}

		rb.next++
	}

	return false
}

func (rb *reassemblyBuffer) retry() bool {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.cfg.Handler.RetryAttempts == 0 {
		return false
	}

	if _, exists := rb.data[rb.next]; exists {
		return false
	}

	for _, dg := range rb.temp {
		if dg.Number == rb.next {
			return false
		}
	}

	if rb.next == rb.pending {
		if rb.retries >= rb.cfg.Handler.RetryAttempts {
			return true
		}

		rb.retries++
	} else {
		rb.pending = rb.next
		rb.retries = 1
	}

	pld := datagram.PayloadRetry{
		Number: rb.next,
	}
	rb.send(datagram.CommandRetry, pld.Marshal())

	return false
}

func (rb *reassemblyBuffer) send(cmd datagram.Cmd, pld []byte) {
	dg := datagram.New(0, 0, cmd, pld)

	if err := rb.ses.WriteRemote(dg); err != nil {
		slog.Error("handler: send", "ses", rb.ses, "cmd", cmd, "err", err)
	}
}
