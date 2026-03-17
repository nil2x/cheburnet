package config

import "flag"

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
