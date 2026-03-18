package config

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Parse parses config values from all supported places and merges it.
// If some of the config fields are not specified, then defaults are used.
//
// Command-line flags are outside of the config and should be parsed
// separately using ParseFlags.
func Parse(file string) (Config, error) {
	cfg, err := parseJSON(file)

	if err != nil {
		return Config{}, fmt.Errorf("json: %v", err)
	}

	env, err := parseEnv()

	if err != nil {
		return Config{}, fmt.Errorf("env: %v", err)
	}

	if env.SocksHost != "" {
		cfg.Socks.Host = env.SocksHost
	}

	if env.SocksPort != 0 {
		cfg.Socks.Port = env.SocksPort
	}

	return cfg, nil
}

// ParseFlags parses and returns command-line flags.
func ParseFlags() Flags {
	flags := Flags{}

	flag.StringVar(
		&flags.ConfigPath,
		"config",
		"config.json",
		"path to configuration file",
	)
	flag.BoolVar(
		&flags.PrintVersion,
		"version",
		false,
		"print program version",
	)
	flag.BoolVar(
		&flags.GenerateSecret,
		"secret",
		false,
		"generate session secret",
	)

	flag.Parse()

	return flags
}

func parseJSON(name string) (Config, error) {
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

func parseEnv() (Env, error) {
	env := Env{
		SocksHost: os.Getenv("SOCKS_HOST"),
	}

	if port := os.Getenv("SOCKS_PORT"); port != "" {
		p, err := strconv.Atoi(port)

		if err != nil {
			return Env{}, err
		}

		env.SocksPort = uint16(p)
	}

	return env, nil
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

// SetupLog initializes logger.
//
// Should be called at the program start with valid config.
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

// SetupDNS initializes DNS.
//
// Should be called at the program start with valid config.
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
