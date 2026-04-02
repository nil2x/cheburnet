package handler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nil2x/cheburnet/internal/api"
	"github.com/nil2x/cheburnet/internal/config"
	"github.com/nil2x/cheburnet/internal/imap"
	"github.com/nil2x/cheburnet/internal/session"
)

// ListenLongPoll listens Long Poll for new datagrams and handles them.
func ListenLongPoll(ctx context.Context, cfg config.Config, vkC *api.VKClient, storageC *api.StorageClient, club config.Club) error {
	server, err := vkC.GroupsGetLongPollServer(club)

	if err != nil {
		return fmt.Errorf("club %v: %v", club.Name, err)
	}

	var sleep time.Duration
	last := api.GroupsUseLongPollServerResponse{
		TS: server.TS,
	}
	fails := 0

	slog.Info("long poll: listening", "club", club.Name)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(sleep):
			last, err = vkC.GroupsUseLongPollServer(ctx, server, last)

			if err != nil {
				slog.Error("long poll: listen", "club", club.Name, "err", err)

				sleep = 5 * time.Second
				fails++

				if fails >= 3 {
					last.Failed = 1
				} else {
					continue
				}
			}

			fails = 0

			if last.Failed != 0 {
				slog.Debug("long poll: refresh", "club", club.Name)

				server, err = vkC.GroupsGetLongPollServer(club)

				if err == nil {
					last = api.GroupsUseLongPollServerResponse{
						TS: server.TS,
					}
					sleep = 0
				} else {
					slog.Error("long poll: refresh", "club", club.Name, "err", err)
					sleep = 5 * time.Second
				}

				continue
			}

			for _, upd := range last.Updates {
				evt := event{
					name:           upd.Type,
					source:         club.Name,
					longPollUpdate: upd,
				}

				go func(evt event) {
					if err := handleEvent(cfg, vkC, storageC, evt); err != nil {
						slog.Error("handler: handle", "event", evt, "err", err)
					}
				}(evt)
			}

			sleep = 0
		}
	}
}

// ListenStorage listens Storage for new datagrams and handles them.
func ListenStorage(ctx context.Context, cfg config.Config, vkC *api.VKClient, storageC *api.StorageClient, club config.Club) error {
	params := api.StorageGetParams{
		Keys: storageC.CreateGetKeys(),
	}
	last, err := vkC.StorageGet(club, params)

	if err != nil {
		return fmt.Errorf("club %v: %v", club.Name, err)
	}

	var sleep time.Duration

	slog.Info("storage: listening", "club", club.Name)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(sleep):
			if !session.IsOpened() {
				sleep = 500 * time.Millisecond
				continue
			}

			current, err := vkC.StorageGet(club, params)

			if err != nil {
				slog.Error("storage: listen", "club", club.Name, "err", err)
				sleep = 5 * time.Second
				continue
			}

			changed := storageC.DiffValues(last, current)
			last = current

			for _, resp := range changed {
				if resp.Value == "" {
					continue
				}

				storageC.UpdateNamespace(resp.Value)

				evt := event{
					name:         "storage",
					source:       club.Name,
					storageValue: resp.Value,
				}

				go func(evt event) {
					if err := handleEvent(cfg, vkC, storageC, evt); err != nil {
						slog.Error("handler: handle", "event", evt, "err", err)
					}
				}(evt)
			}

			sleep = 500 * time.Millisecond
		}
	}
}

// ListenIMAP listens IMAP for new datagrams and handles them.
func ListenIMAP(ctx context.Context, cfg config.Config, vkC *api.VKClient, storageC *api.StorageClient, imapC *imap.Client) error {
	last, err := imapC.Status()

	if err != nil {
		return fmt.Errorf("status: %v", err)
	}

	var sleep time.Duration

	slog.Info("imap: listening", "name", imapC.Name)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(sleep):
			if !session.IsOpened() {
				sleep = 1000 * time.Millisecond
				continue
			}

			current, err := imapC.Status()

			if err != nil {
				slog.Error("imap: listen", "name", imapC.Name, "err", err)
				sleep = 5 * time.Second
				continue
			}

			if current.UIDNext == last.UIDNext {
				// Some servers (e.g., Rambler) may return cached status if
				// the client did not perform any actions. Let's perform no-op
				// to trigger cache revalidation.
				if err := imapC.NoOp(); err != nil {
					slog.Error("imap: listen", "name", imapC.Name, "err", err)
				}

				sleep = 1000 * time.Millisecond
				continue
			}

			messages, err := imapC.Fetch(last.UIDNext, current.UIDNext)

			if err == nil {
				// Sometimes returned body may be zero length.
				// But IMAP logs show that fetched body is non-zero.
				// Most probably there is a bug somewhere in the Go IMAP library.
				// Let's fetch again, usually this helps.
				for _, m := range messages {
					if len(m.Body) == 0 {
						messages, err = imapC.Fetch(last.UIDNext, current.UIDNext)
						break
					}
				}
			}

			if err != nil {
				slog.Error("imap: listen", "name", imapC.Name, "err", err)
				sleep = 5 * time.Second
				continue
			}

			for _, msg := range messages {
				evt := event{
					name:      "imap",
					source:    imapC.Name,
					imapValue: msg.Body,
				}

				go func(evt event) {
					if err := handleEvent(cfg, vkC, storageC, evt); err != nil {
						slog.Error("handler: handle", "event", evt, "err", err)
					}
				}(evt)
			}

			last = current
			sleep = 1000 * time.Millisecond
		}
	}
}
