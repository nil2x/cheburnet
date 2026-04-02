package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/nil2x/cheburnet/internal/api"
	"github.com/nil2x/cheburnet/internal/config"
	"github.com/nil2x/cheburnet/internal/datagram"
	"github.com/nil2x/cheburnet/internal/session"
	"github.com/nil2x/cheburnet/internal/socks"
	"github.com/nil2x/cheburnet/internal/transform"
)

type event struct {
	name           string
	source         string
	longPollUpdate api.Update
	storageValue   string
	imapValue      string
}

func (e event) String() string {
	return fmt.Sprintf("name=%v source=%v", e.name, e.source)
}

// handleEvent accepts event that contain datagrams data, extracts them and
// executes their processing using reassemblyBuffer. Loopback and zero datagrams
// are skipped.
//
// This function should be executed in goroutine as handling of one event may
// take some time.
func handleEvent(cfg config.Config, vkC *api.VKClient, storageC *api.StorageClient, evt event) error {
	muxed := ""
	encoded := []string{}
	var err error

	if len(evt.longPollUpdate.Type) > 0 {
		switch evt.longPollUpdate.TypeEnum() {
		case api.UpdateTypeMessageReply:
			muxed = evt.longPollUpdate.Object.Text
		case api.UpdateTypeWallPostNew:
			muxed = evt.longPollUpdate.Object.Text
		case api.UpdateTypeWallReplyNew:
			muxed = evt.longPollUpdate.Object.Text
		case api.UpdateTypePhotoNew:
			if transform.IsTextURL(evt.longPollUpdate.Object.Text) {
				muxed = evt.longPollUpdate.Object.Text
			} else if datagram.IsMuxed(evt.longPollUpdate.Object.Text) {
				muxed = evt.longPollUpdate.Object.Text
			} else if shouldHandlePhoto(evt.longPollUpdate.Object.Text) {
				encoded, err = handlePhoto(cfg, vkC, evt.longPollUpdate.Object.OrigPhoto.URL)
			} else {
				muxed = evt.longPollUpdate.Object.Text
			}
		case api.UpdateTypeGroupChangeSettings:
			if len(evt.longPollUpdate.Object.Changes.Description.NewValue) > 0 {
				muxed = evt.longPollUpdate.Object.Changes.Description.NewValue
			} else if len(evt.longPollUpdate.Object.Changes.Website.NewValue) > 0 {
				muxed = evt.longPollUpdate.Object.Changes.Website.NewValue
			}
		case api.UpdateTypeVideoCommentNew:
			muxed = evt.longPollUpdate.Object.Text
		case api.UpdateTypePhotoCommentNew:
			muxed = evt.longPollUpdate.Object.Text
		case api.UpdateTypeMarketCommentNew:
			muxed = evt.longPollUpdate.Object.Text
		case api.UpdateTypeBoardPostNew:
			muxed = evt.longPollUpdate.Object.Text
		default:
			err = errors.New("unsupported update")
		}
	} else if len(evt.storageValue) > 0 {
		muxed = evt.storageValue
	} else if len(evt.imapValue) > 0 {
		muxed = evt.imapValue
	} else {
		err = errors.New("empty event")
	}

	if transform.IsTextURL(muxed) {
		uri := transform.FromTextURL(muxed)

		if shouldHandleDoc(uri) {
			muxed, err = handleDoc(cfg, vkC, uri)
		} else {
			muxed = ""
		}
	}

	if err != nil {
		return err
	}

	if len(muxed) > 0 {
		encoded = append(encoded, datagram.Demux(muxed)...)
	}

	var datagrams []datagram.Datagram

	for _, enc := range encoded {
		dg, err := handleEncoded(enc)

		if err != nil {
			return err
		}

		if !dg.IsZero() {
			datagrams = append(datagrams, dg)
		}
	}

	sort.Slice(datagrams, func(i, j int) bool {
		return datagrams[i].Number < datagrams[j].Number
	})

	for _, dg := range datagrams {
		slog.Debug("handler: handle", "event", evt, "dg", dg)

		if err := handleDatagram(cfg, vkC, storageC, dg); err != nil {
			slog.Error("handler: handle", "event", evt, "dg", dg, "err", err)
		}
	}

	return nil
}

func shouldHandlePhoto(caption string) bool {
	if len(caption) == 0 {
		return true
	}

	dg, err := handleEncoded(caption)

	if err != nil {
		return true
	}

	sentByMethodQR := !dg.IsZero() && dg.Command == 0

	return sentByMethodQR
}

func handlePhoto(cfg config.Config, vkC *api.VKClient, uri string) ([]string, error) {
	var b []byte
	var err error

	for i := 0; i != cfg.Handler.DownloadAttempts; i++ {
		b, err = vkC.Download(uri)

		if err == nil || api.IsFloodControl(err) {
			break
		}
	}

	if err != nil {
		return nil, fmt.Errorf("download url: %v", err)
	}

	file, err := transform.SaveQR(b, "jpg", cfg.QR.SaveDir)

	if err != nil {
		return nil, fmt.Errorf("save qr: %v", err)
	}

	defer os.Remove(file)

	content, err := transform.DecodeQR(file, cfg.QR.ZBarPath)

	if err != nil {
		return nil, fmt.Errorf("decode qr: %v", err)
	}

	return content, nil
}

func shouldHandleDoc(uri string) bool {
	query, err := transform.ExtractQuery(uri)

	if err != nil {
		return true
	}

	if len(query.Caption) == 0 {
		return true
	}

	dg, err := handleEncoded(query.Caption)

	if err != nil {
		return true
	}

	sentByMethodDoc := !dg.IsZero()

	return sentByMethodDoc
}

func handleDoc(cfg config.Config, vkC *api.VKClient, uri string) (string, error) {
	var err error
	uri, err = transform.DeleteQuery(uri)

	if err != nil {
		return "", fmt.Errorf("delete query: %v", err)
	}

	var b []byte

	for i := 0; i != cfg.Handler.DownloadAttempts; i++ {
		b, err = vkC.Download(uri)

		if err == nil || api.IsFloodControl(err) {
			break
		}
	}

	if err != nil {
		return "", fmt.Errorf("download url: %v", err)
	}

	return string(b), nil
}

func handleEncoded(s string) (datagram.Datagram, error) {
	dg, err := datagram.Decode(s)

	if err != nil {
		return datagram.Datagram{}, fmt.Errorf("decode datagram: %v", err)
	}

	if dg.IsLoopback() {
		return datagram.Datagram{}, nil
	}

	return dg, nil
}

var handleDatagramMu = sync.Mutex{}
var handleDatagramBuffers = map[datagram.Ses]*reassemblyBuffer{}

func handleDatagram(cfg config.Config, vkC *api.VKClient, storageC *api.StorageClient, dg datagram.Datagram) error {
	handleDatagramMu.Lock()
	defer handleDatagramMu.Unlock()

	ses, exists := session.Get(dg.Session)

	if exists && ses.IsClosed() && dg.Command == datagram.CommandConnect {
		slog.Warn("handler: bidirectional proxying is not supported")
		exists = false
	}

	if !exists {
		var err error
		ses, err = session.Open(cfg, vkC, storageC, dg.Session)

		if err != nil {
			return fmt.Errorf("open session: %v", err)
		}

		session.Set(ses.ID, ses)
		delete(handleDatagramBuffers, ses.ID)
	}

	buffer, exists := handleDatagramBuffers[ses.ID]

	if !exists {
		buffer = openReassemblyBuffer(cfg, ses)
		handleDatagramBuffers[ses.ID] = buffer
	}

	if err := buffer.push(dg); err != nil {
		return fmt.Errorf("buffer push: %v", err)
	}

	return nil
}

func handleCommand(cfg config.Config, ses *session.Session, dg datagram.Datagram) error {
	slog.Debug("handler: command", "dg", dg)

	if cfg.Log.Payload {
		slog.Debug("handler: payload", "ses", ses, "in", transform.BytesToHex(dg.Payload))
	}

	var err error

	switch dg.Command {
	case datagram.CommandConnect:
		err = handleConnect(cfg, ses, dg)
	case datagram.CommandForward:
		err = handleForward(ses, dg)
	case datagram.CommandClose:
		err = handleClose(ses)
	case datagram.CommandRetry:
		err = handleRetry(ses, dg)
	default:
		err = errors.New("unsupported")
	}

	if dg.Command == datagram.CommandConnect && err == nil {
		slog.Info("handler: forwarding", "ses", ses)
	}

	if err != nil {
		return fmt.Errorf("command %v: %v", dg.Command, err)
	}

	return nil
}

func handleConnect(cfg config.Config, ses *session.Session, dg datagram.Datagram) error {
	decrypted, err := transform.Decrypt(dg.Payload, cfg.Session.SecretKey)

	if err != nil {
		return err
	}

	pld := datagram.PayloadConnect{}

	if err := pld.Unmarshal(decrypted); err != nil {
		return err
	}

	addr := config.Address(pld).String()
	timeout := cfg.Handler.ConnectTimeout()

	var conn net.Conn

	if timeout > 0 {
		conn, err = net.DialTimeout("tcp", addr, timeout)
	} else {
		conn, err = net.Dial("tcp", addr)
	}

	if err != nil {
		return err
	}

	ses.SetLocal(conn, func(c net.Conn, b []byte) error {
		return socks.Write(cfg, c, b)
	})

	socks.Forward(cfg, ses, conn)

	return nil
}

func handleForward(ses *session.Session, dg datagram.Datagram) error {
	return ses.WriteLocal(dg.Payload)
}

func handleClose(ses *session.Session) error {
	ses.Close()

	return nil
}

func handleRetry(ses *session.Session, dg datagram.Datagram) error {
	pld := datagram.PayloadRetry{}

	if err := pld.Unmarshal(dg.Payload); err != nil {
		return err
	}

	var err error
	dg, exists := ses.GetHistory(pld.Number)

	if ses.IsClosed() {
		if exists {
			err = session.WriteRemote(ses, dg)
		} else {
			dg := datagram.New(0, pld.Number, datagram.CommandClose, nil)
			err = session.WriteRemote(ses, dg)
		}
	} else {
		if exists {
			err = ses.WriteRemote(dg)
		} else {
			slog.Debug("handler: history miss", "ses", ses, "number", pld.Number)
		}
	}

	return err
}

// Clear periodically clears the global state.
func Clear(ctx context.Context) error {
	deleteInterval := 2 * time.Minute
	deleteAge := 2 * time.Minute

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(deleteInterval):
			handleDatagramMu.Lock()

			for ses, buffer := range handleDatagramBuffers {
				if buffer.isClosed() && buffer.sinceClose() > deleteAge {
					delete(handleDatagramBuffers, ses)
				}
			}

			handleDatagramMu.Unlock()
		}
	}
}
