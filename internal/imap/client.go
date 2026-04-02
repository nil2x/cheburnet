package imap

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/nil2x/cheburnet/internal/config"
)

type UID = imap.UID

type toRemoveItem struct {
	uid      UID
	markedAt time.Time
}

// Client is an IMAP client to interact with IMAP server.
// It is safe for concurrent use, but take care of IMAP commands order.
type Client struct {
	Name        string
	cfg         config.IMAP
	client      *imapclient.Client
	debugWriter io.WriteCloser
	mu          sync.Mutex
	isClosed    bool
	toRemove    []toRemoveItem
}

// Open opens a new connection to IMAP server and performs login.
// Use returned client to interact with the server and the mailbox.
// You should close the client when you done.
func Open(cfg config.IMAP) (*Client, error) {
	options := &imapclient.Options{}
	var debugWriter io.WriteCloser

	if cfg.Debug {
		debugWriter = newDebugWriter(cfg.Name)
		options.DebugWriter = debugWriter
	}

	addr := cfg.Address.String()
	var client *imapclient.Client
	var err error

	if cfg.Insecure {
		client, err = imapclient.DialInsecure(addr, options)
	} else {
		client, err = imapclient.DialTLS(addr, options)
	}

	if err != nil {
		return nil, fmt.Errorf("dial: %v", err)
	}

	if err := client.Login(cfg.Username, cfg.Password).Wait(); err != nil {
		return nil, fmt.Errorf("log in: %v", err)
	}

	if _, err := client.Select(cfg.Mailbox, nil).Wait(); err != nil {
		if strings.Contains(err.Error(), "doesn't exist") {
			err = errors.New("mailbox doesn't exist")
		}

		return nil, fmt.Errorf("select: %v", err)
	}

	c := &Client{
		Name:        cfg.Name,
		cfg:         cfg,
		client:      client,
		debugWriter: debugWriter,
		mu:          sync.Mutex{},
		isClosed:    false,
		toRemove:    []toRemoveItem{},
	}

	return c, nil
}

// Close closes the client. After close you shouldn't interact with the server.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isClosed {
		return nil
	}

	if c.client == nil {
		return nil
	}

	if err := c.client.Logout().Wait(); err != nil {
		return fmt.Errorf("log out: %v", err)
	}

	if err := c.client.Close(); err != nil {
		return fmt.Errorf("close: %v", err)
	}

	if c.debugWriter != nil {
		if err := c.debugWriter.Close(); err != nil {
			return fmt.Errorf("debug writer: %v", err)
		}
	}

	c.isClosed = true

	return nil
}

// NoOp performs NOOP command.
func (c *Client) NoOp() error {
	return c.client.Noop().Wait()
}

type Mailbox struct {
	Name     string
	NoSelect bool
	Drafts   bool
}

// List performs LIST command. Result is an information about all top-level folders.
func (c *Client) List() ([]Mailbox, error) {
	items, err := c.client.List("", "%", nil).Collect()

	if err != nil {
		return nil, err
	}

	boxes := make([]Mailbox, 0, len(items))

	for _, item := range items {
		box := Mailbox{
			Name:     item.Mailbox,
			NoSelect: slices.Contains(item.Attrs, imap.MailboxAttrNoSelect),
			Drafts:   slices.Contains(item.Attrs, imap.MailboxAttrDrafts),
		}
		boxes = append(boxes, box)
	}

	return boxes, nil
}

// Append performs APPEND command on the configured mailbox.
// Created message is flagged as a draft.
//
// Returns UID of the created message. If the server does not support returning UID,
// 0 is returned with no error.
//
// text is a plain text body of the message in UTF-8 encoding.
// From, To and Subject are taken from config or fallback to default values.
func (c *Client) Append(text string) (UID, error) {
	from := c.cfg.From
	to := c.cfg.To
	subject := c.cfg.Subject

	if from == "" {
		from = c.cfg.Username
	}

	if to == "" {
		to = "stub@stub"
	}

	if subject == "" {
		subject = "stub"
	}

	var email string
	sep := "\r\n"

	email += "From: " + from + sep
	email += "To: " + to + sep
	email += "Subject: " + mime.QEncoding.Encode("utf-8", subject) + sep
	email += "MIME-Version: 1.0" + sep
	email += "Content-Type: text/plain; charset=\"utf-8\"" + sep
	email += "Content-Transfer-Encoding: 8bit" + sep
	email += sep
	email += text

	data := []byte(email)
	size := int64(len(data))
	options := &imap.AppendOptions{
		Flags: []imap.Flag{imap.FlagDraft},
		Time:  time.Now().UTC(),
	}

	cmd := c.client.Append(c.cfg.Mailbox, size, options)

	if _, err := cmd.Write(data); err != nil {
		return 0, fmt.Errorf("write: %v", err)
	}

	if err := cmd.Close(); err != nil {
		return 0, fmt.Errorf("close: %v", err)
	}

	result, err := cmd.Wait()

	if err != nil {
		return 0, fmt.Errorf("wait: %v", err)
	}

	return result.UID, nil
}

type Status struct {
	NumMessages int
	UIDNext     UID
}

// Status performs STATUS command on the configured mailbox.
func (c *Client) Status() (Status, error) {
	options := &imap.StatusOptions{
		NumMessages: true,
		UIDNext:     true,
	}
	data, err := c.client.Status(c.cfg.Mailbox, options).Wait()

	if err != nil {
		return Status{}, err
	}

	status := Status{
		NumMessages: int(*data.NumMessages),
		UIDNext:     data.UIDNext,
	}

	return status, nil
}

type Message struct {
	UID  UID
	Body string
}

// Fetch performs FETCH command on the configured mailbox.
// Returned messages are flagged as seen.
//
// start and stop specifies range bounds, both are inclusive.
// For example 100:102 will return 3 messages if they exists: 100, 101, 102.
// If you specify 0 for stop, this will return all messages starting from start.
func (c *Client) Fetch(start, stop UID) ([]Message, error) {
	numSet := imap.UIDSet{}
	numSet.AddRange(start, stop)

	bodySection := &imap.FetchItemBodySection{
		Specifier: imap.PartSpecifierText,
	}
	options := &imap.FetchOptions{
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{bodySection},
	}

	cmd := c.client.Fetch(numSet, options)
	defer cmd.Close()

	messages := []Message{}

	for {
		item := cmd.Next()

		if item == nil {
			break
		}

		msg := Message{}

		for {
			data := item.Next()

			if data == nil {
				break
			}

			switch data := data.(type) {
			case imapclient.FetchItemDataUID:
				msg.UID = data.UID
			case imapclient.FetchItemDataBodySection:
				b, err := io.ReadAll(data.Literal)

				if err != nil {
					return nil, fmt.Errorf("message %v: read body: %v", msg.UID, err)
				}

				msg.Body = string(b)
			}
		}

		messages = append(messages, msg)
	}

	if err := cmd.Close(); err != nil {
		return nil, fmt.Errorf("close: %v", err)
	}

	return messages, nil
}

// MarkToRemove schedules removal of message with given uid.
// To execute deletion of scheduled items use RemoveMarked.
//
// Zero uid is not allowed as it means "all messages".
func (c *Client) MarkToRemove(uid UID) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if uid == 0 {
		return fmt.Errorf("zero UID is not allowed")
	}

	item := toRemoveItem{
		uid:      uid,
		markedAt: time.Now(),
	}
	c.toRemove = append(c.toRemove, item)

	return nil
}

// RemoveMarked executes command to remove items that were marked using MarkToRemove.
//
// age specifies minimum item age since it was marked. For example, 60 seconds age will
// result in removal of items that were marked >= 60 seconds ago. Zero age results in removal
// of all marked items despite of their age.
func (c *Client) RemoveMarked(age time.Duration) error {
	c.mu.Lock()

	remove := []UID{}
	postpone := []toRemoveItem{}

	for _, item := range c.toRemove {
		if time.Since(item.markedAt) < age {
			postpone = append(postpone, item)
		} else {
			remove = append(remove, item.uid)
		}
	}

	c.toRemove = postpone

	c.mu.Unlock()

	if len(remove) == 0 {
		return nil
	}

	numSet := imap.UIDSet{}
	numSet.AddNum(remove...)

	flags := &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Flags:  []imap.Flag{imap.FlagDeleted},
		Silent: true,
	}

	if err := c.client.Store(numSet, flags, nil).Close(); err != nil {
		return fmt.Errorf("store: %v", err)
	}

	if err := c.client.UIDExpunge(numSet).Close(); err != nil {
		return fmt.Errorf("expunge: %v", err)
	}

	return nil
}

func newDebugWriter(name string) io.WriteCloser {
	r, w := io.Pipe()

	go func() {
		buf := make([]byte, 1024)

		for {
			n, err := r.Read(buf)

			if err == io.EOF {
				break
			} else if err != nil {
				slog.Error("imap: debug", "name", name, "err", err)
				break
			}

			isDatagramPayload := !bytes.ContainsRune(buf[:n], ' ')

			// Do not flood the log with datagram payload.
			if isDatagramPayload {
				continue
			}

			s := string(buf[:n])
			msg := "imap: debug: " + strings.ReplaceAll(s, "\r\n", "")

			slog.Debug(msg, "name", name)
		}
	}()

	return w
}
