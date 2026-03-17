package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"
)

// Config holds configuration of various parts of the program.
// See README for fields description.
type Config struct {
	Log     Log     `json:"log"`
	DNS     DNS     `json:"dns"`
	Session Session `json:"session"`
	Socks   Socks   `json:"socks"`
	API     API     `json:"api"`
	QR      QR      `json:"qr"`
	Clubs   []Club  `json:"clubs"`
	Users   []User  `json:"users"`
}

type Log struct {
	Level   int    `json:"level"`
	Output  string `json:"output"`
	Payload bool   `json:"payload"`
}

type DNS struct {
	Address
	Provider string `json:"provider"`
}

type Session struct {
	TimeoutMS int    `json:"timeout"`
	Secret    string `json:"secret"`
	SecretKey []byte `json:"-"`
}

func (cfg Session) Timeout() time.Duration {
	return time.Duration(cfg.TimeoutMS) * time.Millisecond
}

type Socks struct {
	Address
	ForwardSize       int `json:"forwardSize"`
	ForwardIntervalMS int `json:"forwardInterval"`
}

func (cfg Socks) ForwardInterval() time.Duration {
	return time.Duration(cfg.ForwardIntervalMS) * time.Millisecond
}

type Address struct {
	Host string `json:"host"`
	Port uint16 `json:"port"`
}

func (a Address) String() string {
	return net.JoinHostPort(a.Host, fmt.Sprint(a.Port))
}

type API struct {
	TimeoutMS   int  `json:"-"`
	Unathorized bool `json:"unathorized"`
}

func (cfg API) Timeout() time.Duration {
	return time.Duration(cfg.TimeoutMS) * time.Millisecond
}

type QR struct {
	ZBarPath   string `json:"zbarPath"`
	ImageSize  int    `json:"-"`
	ImageLevel int    `json:"-"`
	SaveDir    string `json:"saveDir"`
}

type Club struct {
	Name        string `json:"name"`
	ID          string `json:"id"`
	AccessToken string `json:"accessToken"`
	AlbumID     string `json:"albumID"`
	PhotoID     string `json:"photoID"`
	VideoID     string `json:"videoID"`
	MarketID    string `json:"marketID"`
}

type User struct {
	Name        string `json:"name"`
	ID          string `json:"id"`
	AccessToken string `json:"accessToken"`
}

func defaultConfig() Config {
	return Config{
		Log: Log{
			Level: 0,
		},
		Session: Session{
			TimeoutMS: 30 * 1000,
		},
		Socks: Socks{
			Address: Address{
				Host: "127.0.0.1",
				Port: 1080,
			},
			ForwardSize:       1 * 1024 * 1024,
			ForwardIntervalMS: 500,
		},
		API: API{
			TimeoutMS: 10 * 1000,
		},
		QR: QR{
			ZBarPath:   "zbarimg",
			ImageSize:  512,
			ImageLevel: 1,
		},
	}
}

// Parse parses JSON file at the given path and returns parsed Config.
// If the file not specifies some of the fields, then defaults will be used.
func Parse(name string) (Config, error) {
	data, err := os.ReadFile(name)

	if err != nil {
		return Config{}, err
	}

	cfg := defaultConfig()

	if len(data) == 0 {
		return cfg, nil
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Validate checks config fields that are essential for the program,
// so that they have correct values and the program will function correctly.
func Validate(cfg Config) error {
	if len(cfg.Clubs) == 0 {
		return errors.New("clubs are missing")
	}

	if len(cfg.Users) == 0 {
		return errors.New("users are missing")
	}

	for _, club := range cfg.Clubs {
		if club.Name == "" {
			return errors.New("club.name is missing")
		}

		if club.ID == "" {
			return errors.New("club.id is missing")
		}

		if club.AccessToken == "" {
			return errors.New("club.accessToken is missing")
		}

		if club.AlbumID == "" {
			return errors.New("club.albumID is missing")
		}

		if club.PhotoID == "" {
			return errors.New("club.photoID is missing")
		}

		if club.VideoID == "" {
			return errors.New("club.videoID is missing")
		}

		if club.MarketID == "" {
			return errors.New("club.marketID is missing")
		}
	}

	for _, user := range cfg.Users {
		if user.Name == "" {
			return errors.New("user.name is missing")
		}

		if user.ID == "" {
			return errors.New("user.id is missing")
		}

		if user.AccessToken == "" && !cfg.API.Unathorized {
			return errors.New("user.accessToken is missing")
		}
	}

	if len(cfg.Session.SecretKey) == 0 {
		return errors.New("session.secret is missing")
	}

	return nil
}

func SetupLog(cfg Log) error {
	if cfg.Output == "" {
		slog.SetLogLoggerLevel(slog.Level(cfg.Level))
		return nil
	}

	f, err := os.OpenFile(cfg.Output, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)

	if err != nil {
		return err
	}

	handler := slog.NewTextHandler(f, &slog.HandlerOptions{
		Level: slog.Level(cfg.Level),
	})
	logger := slog.New(handler)

	slog.SetDefault(logger)

	return nil
}

func SetupDNS(cfg DNS) error {
	switch strings.ToLower(cfg.Provider) {
	case "":
	case "yandex":
		cfg.Host = "77.88.8.8"
		cfg.Port = 53
	case "msk-ix":
		cfg.Host = "62.76.76.62"
		cfg.Port = 53
	case "google":
		cfg.Host = "8.8.8.8"
		cfg.Port = 53
	case "cloudflare":
		cfg.Host = "1.1.1.1"
		cfg.Port = 53
	default:
		return fmt.Errorf("unknown dns provider: %v", cfg.Provider)
	}

	if cfg.Host == "" {
		return nil
	}

	if cfg.Port == 0 {
		cfg.Port = 53
	}

	d := net.Dialer{
		Timeout: time.Second * 3,
	}
	addr := cfg.Address.String()

	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return d.DialContext(ctx, "udp", addr)
		},
	}

	return nil
}
