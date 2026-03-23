package handler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nil2x/cheburnet/internal/api"
	"github.com/nil2x/cheburnet/internal/config"
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
				continue
			}

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
					club:           club,
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
					club:         club,
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
