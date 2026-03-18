package config

import (
	"flag"
	"os"
	"strconv"
)

type Flags struct {
	ConfigPath     string
	PrintVersion   bool
	GenerateSecret bool
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

type Env struct {
	SocksPort uint16
}

// ParseEnv parses and returns environment variables.
func ParseEnv() (Env, error) {
	env := Env{}

	if port := os.Getenv("SOCKS_PORT"); port != "" {
		p, err := strconv.Atoi(port)

		if err != nil {
			return Env{}, err
		}

		env.SocksPort = uint16(p)
	}

	return env, nil
}
