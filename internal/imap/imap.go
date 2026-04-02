package imap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/nil2x/cheburnet/internal/config"
)

// Init calls Open and SetClient for every passed config.
//
// Should be called at the program start before the package usage.
func Init(configs []config.IMAP) error {
	for _, cfg := range configs {
		client, err := Open(cfg)

		if err != nil {
			return fmt.Errorf("%v: open: %v", cfg.Name, err)
		}

		SetClient(cfg.Name, client)
	}

	return nil
}

// Close calls RemoveMarked and Close for all clients in the global state.
// At the end clears the global state.
//
// Should be called at the program end after you done using the package.
func Close() error {
	for _, client := range GetClients() {
		if err := client.RemoveMarked(0); err != nil {
			slog.Error("imap: close", "name", client.Name, "err", err)
		}

		if err := client.Close(); err != nil {
			slog.Error("imap: close", "name", client.Name, "err", err)
		}
	}

	clearClients()

	return nil
}

// Clear periodically calls RemoveMarked for all clients in the global state.
func Clear(ctx context.Context) error {
	deleteInterval := 2 * time.Minute
	deleteAge := 2 * time.Minute

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(deleteInterval):
			for _, client := range GetClients() {
				if err := client.RemoveMarked(deleteAge); err != nil {
					slog.Error("imap: clear", "name", client.Name, "err", err)
				}
			}
		}
	}
}

// Validate checks that the given client is valid for usage with the package.
func Validate(client *Client) error {
	if client.client == nil {
		return errors.New("client is not opened")
	}

	if err := client.NoOp(); err != nil {
		return fmt.Errorf("no-op: %v", err)
	}

	caps := client.client.Caps()

	if _, exists := caps[imap.CapIMAP4rev2]; !exists {
		if _, exists := caps[imap.CapUIDPlus]; !exists {
			return fmt.Errorf("server does not support %v", imap.CapUIDPlus)
		}
	}

	boxes, err := client.List()

	if err != nil {
		return fmt.Errorf("list: %v", err)
	}

	var box Mailbox

	for _, b := range boxes {
		if b.Name == client.cfg.Mailbox {
			box = b
			break
		}
	}

	if box.Name == "" {
		return errors.New("mailbox not found")
	}

	if box.NoSelect {
		return errors.New("mailbox is no-select")
	}

	if !box.Drafts {
		return errors.New("mailbox does not support drafts")
	}

	return nil
}
