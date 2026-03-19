package socks

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/nil2x/cheburnet/internal/api"
	"github.com/nil2x/cheburnet/internal/config"
	"github.com/nil2x/cheburnet/internal/datagram"
	"github.com/nil2x/cheburnet/internal/session"
	"github.com/nil2x/cheburnet/internal/transform"
	"golang.org/x/time/rate"
)

type socksStage int

const (
	stageHandshake socksStage = iota
	stageConnectV4
	stageConnectV5
	stageConnectSession
	stageForward
)

var (
	errUnacceptable = errors.New("unacceptable")
	errUnsupported  = errors.New("unsupported")
	errPartialRead  = errors.New("partial read")
)

// Listen starts listener on the configured address and accepts SOCKS connections.
// For every new connection a Session is created. This connection and session are
// tied together. Every local read from the connection will produce remote write
// to the session.
//
// All SOCKS versions are supported: 4, 4a, 5. Only TCP connection stream with
// no authentication is supported. Everything else is not supported.
func Listen(ctx context.Context, cfg config.Config, vkC *api.VKClient, storageC *api.StorageClient) error {
	addr := cfg.Socks.Address.String()
	ln, err := net.Listen("tcp", addr)

	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	var limiter *rate.Limiter

	if cfg.Socks.AcceptRate > 0 {
		limiter = rate.NewLimiter(rate.Limit(cfg.Socks.AcceptRate), cfg.Socks.AcceptRate)
	}

	slog.Info("socks: listening", "addr", addr)

	for {
		conn, err := ln.Accept()

		if err != nil {
			slog.Error("socks: accept", "err", err)
			continue
		}

		if limiter != nil && !limiter.Allow() {
			slog.Debug("socks: accept: rate limit")
			conn.Close()
			continue
		}

		ses, err := session.Open(cfg, vkC, storageC, session.NextID())

		if err != nil {
			slog.Error("socks: session", "err", err)
			conn.Close()
			continue
		}

		ses.SetLocal(conn, func(c net.Conn, b []byte) error {
			return Write(cfg, c, b)
		})
		session.Set(ses.ID, ses)

		go accept(cfg, ses, conn, stageHandshake)
	}
}

// Forward accepts established connection without performing SOCKS handshake.
// Every local read from the connection will produce remote write to the session.
//
// This function is non-blocking.
func Forward(cfg config.Config, ses *session.Session, conn net.Conn) {
	go accept(cfg, ses, conn, stageForward)
}

func accept(cfg config.Config, ses *session.Session, peer net.Conn, stage socksStage) {
	addr := peer.RemoteAddr().String()

	defer slog.Info("socks: closed", "peer", addr, "ses", ses)
	defer ses.Close()

	slog.Debug("socks: accept", "peer", addr, "ses", ses)

	if err := handle(cfg, ses, peer, stage); err != nil {
		slog.Error("socks: handle", "peer", addr, "ses", ses, "err", err)
	}
}

type buffer struct {
	buf  *bytes.Buffer
	mu   *sync.Mutex
	done chan struct{}
}

func handle(cfg config.Config, ses *session.Session, peer net.Conn, stage socksStage) error {
	var wg sync.WaitGroup
	var readErr error
	var fwdErr error
	fwdBuf := buffer{
		buf:  &bytes.Buffer{},
		mu:   &sync.Mutex{},
		done: make(chan struct{}),
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(fwdBuf.done)

		readErr = read(cfg, ses, peer, stage, fwdBuf)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		fwdErr = forward(cfg, ses, fwdBuf)

		if fwdErr != nil {
			peer.Close()
		}
	}()

	wg.Wait()

	return errors.Join(readErr, fwdErr)
}

func read(cfg config.Config, ses *session.Session, peer net.Conn, stage socksStage, fwdBuf buffer) error {
	addr := peer.RemoteAddr().String()
	timeout := cfg.Socks.ReadTimeout()
	temp := make([]byte, cfg.Socks.ReadSize)

	var limiter *rate.Limiter
	var limiterCtx context.Context

	if cfg.Socks.ReadRate > 0 {
		var limiterCancel context.CancelFunc

		limiter = rate.NewLimiter(rate.Limit(cfg.Socks.ReadRate), cfg.Socks.ReadRate)
		limiterCtx, limiterCancel = context.WithCancel(context.Background())

		defer limiterCancel()

		go func() {
			<-ses.OnClose
			limiterCancel()
		}()
	}

	for {
		var deadline time.Time

		if timeout > 0 {
			deadline = time.Now().Add(timeout)
		}

		if err := peer.SetReadDeadline(deadline); err != nil {
			return err
		}

		readN, readErr := peer.Read(temp)

		if readN > 0 {
			in := temp[:readN]

			slog.Debug("socks: read", "peer", addr, "len", len(in))

			if cfg.Log.Payload {
				slog.Debug("socks: payload", "peer", addr, "in", transform.BytesToHex(in))
			}

			if stage == stageHandshake && in[0] == 0x04 {
				stage = stageConnectV4
			}

			var out []byte
			var err error
			var remote config.Address

			switch stage {
			case stageHandshake:
				out, err = handleHandshakeV5(in)
				stage = stageConnectV5
			case stageConnectV4:
				remote, out, err = handleConnectV4(in)

				if err == nil {
					stage = stageConnectSession
				}
			case stageConnectV5:
				remote, out, err = handleConnectV5(in)

				if err == nil {
					stage = stageConnectSession
				}
			}

			switch stage {
			case stageConnectSession:
				err = handleConnectSession(ses, remote, cfg.Session.SecretKey)

				if err == nil {
					slog.Info("socks: forwarding", "peer", addr, "ses", ses, "remote", remote)
					stage = stageForward
				}
			case stageForward:
				fwdBuf.mu.Lock()
				fwdBuf.buf.Write(in)
				fwdBuf.mu.Unlock()
			}

			if len(out) > 0 {
				if writeErr := Write(cfg, peer, out); writeErr != nil && err == nil {
					err = writeErr
				}
			}

			if err != nil {
				return err
			}
		}

		if errors.Is(readErr, io.EOF) {
			return nil
		}

		if readErr != nil {
			return readErr
		}

		if limiter != nil && stage == stageForward {
			if err := limiter.WaitN(limiterCtx, readN); err != nil {
				return err
			}
		}
	}
}

func forward(cfg config.Config, ses *session.Session, buf buffer) error {
	interval := cfg.Socks.ForwardInterval()

	for {
		stop := false

		select {
		case <-buf.done:
			stop = true
		case <-time.After(interval):
		}

		var in []byte

		buf.mu.Lock()

		if buf.buf.Len() > 0 {
			in = bytes.Clone(buf.buf.Bytes())
			buf.buf.Reset()
		}

		buf.mu.Unlock()

		if len(in) > 0 {
			slog.Debug("socks: forward", "ses", ses, "len", len(in))

			err := handleForward(ses, in, cfg.Socks.ForwardSize)

			if err != nil {
				return err
			}
		}

		if stop {
			return nil
		}
	}
}

// Write writes data to the connection.
func Write(cfg config.Config, conn net.Conn, out []byte) error {
	addr := conn.RemoteAddr().String()

	slog.Debug("socks: write", "peer", addr, "len", len(out))

	if cfg.Log.Payload {
		slog.Debug("socks: payload", "peer", addr, "out", transform.BytesToHex(out))
	}

	var deadline time.Time
	timeout := cfg.Socks.WriteTimeout()

	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}

	if err := conn.SetWriteDeadline(deadline); err != nil {
		return err
	}

	_, err := conn.Write(out)

	return err
}

func handleHandshakeV5(in []byte) ([]byte, error) {
	if len(in) < 2 {
		return nil, errPartialRead
	}

	if in[0] != 0x05 {
		return nil, errUnacceptable
	}

	nmethods := int(in[1])

	if len(in) < 2+nmethods {
		return nil, errPartialRead
	}

	methods := in[2 : 2+nmethods]

	if slices.Contains(methods, 0x00) {
		return []byte{0x05, 0x00}, nil
	}

	return []byte{0x05, 0xff}, errUnsupported
}

func handleConnectV4(in []byte) (config.Address, []byte, error) {
	if len(in) < 9 {
		return config.Address{}, nil, errPartialRead
	}

	vn := in[0]

	if vn != 0x04 {
		return config.Address{}, nil, errUnacceptable
	}

	cd := in[1]

	if cd != 0x01 {
		return config.Address{}, nil, errUnsupported
	}

	port := binary.BigEndian.Uint16(in[2:4])
	ip := in[4:8]
	chars := []rune{}

	if ip[0] == 0x00 && ip[1] == 0x00 && ip[2] == 0x00 && ip[3] != 0x00 {
		nulls := 0

		for _, b := range in[8:] {
			if b == 0x00 {
				nulls++
				continue
			}

			if nulls == 1 {
				chars = append(chars, rune(b))
			}
		}
	}

	addr := config.Address{
		Port: port,
	}

	if len(chars) > 0 {
		addr.Host = string(chars)
	} else {
		addr.Host = net.IP(ip).String()
	}

	out := bytes.Clone(in[:8])
	out[0] = 0x00
	out[1] = 0x5a

	return addr, out, nil
}

func handleConnectV5(in []byte) (config.Address, []byte, error) {
	if len(in) < 5 {
		return config.Address{}, nil, errPartialRead
	}

	ver := in[0]

	if ver != 0x05 {
		return config.Address{}, nil, errUnacceptable
	}

	cmd := in[1]

	if cmd != 0x01 {
		return config.Address{}, nil, errUnsupported
	}

	atyp := in[3]
	naddr := 0
	offset := 4

	switch atyp {
	case 0x01:
		naddr = 4
	case 0x03:
		naddr = int(in[4])
		offset = 5
	case 0x04:
		naddr = 16
	default:
		return config.Address{}, nil, errUnsupported
	}

	if len(in) < offset+naddr+2 {
		return config.Address{}, nil, errPartialRead
	}

	baddr := in[offset : offset+naddr]
	addr := ""

	if atyp == 0x03 {
		addr = string(baddr)
	} else {
		addr = net.IP(baddr).String()
	}

	port := binary.BigEndian.Uint16(in[offset+naddr : offset+naddr+2])
	dst := config.Address{
		Host: addr,
		Port: port,
	}

	out := bytes.Clone(in)
	out[1] = 0x00

	return dst, out, nil
}

func handleConnectSession(ses *session.Session, remote config.Address, secretKey []byte) error {
	pld := datagram.PayloadConnect(remote)
	data := pld.Marshal()
	encrypted, err := transform.Encrypt(data, secretKey)

	if err != nil {
		return err
	}

	dg := datagram.New(0, 0, datagram.CommandConnect, encrypted)

	if err := ses.WriteRemote(dg); err != nil {
		return err
	}

	return nil
}

func handleForward(ses *session.Session, in []byte, chunkSize int) error {
	chunks := transform.BytesToChunks(in, chunkSize, 0)

	for _, chunk := range chunks {
		dg := datagram.New(0, 0, datagram.CommandForward, chunk)

		if err := ses.WriteRemote(dg); err != nil {
			return err
		}
	}

	return nil
}
