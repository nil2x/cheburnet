package yadisk

import (
	"github.com/nil2x/cheburnet/internal/config"
)

// Init calls New and SetClient for every passed config.
//
// Should be called at the program start before the package usage.
func Init(cfgAPI config.API, configs []config.YaDisk) error {
	for _, cfg := range configs {
		client := New(cfgAPI, cfg)
		SetClient(cfg.Name, client)
	}

	return nil
}

// Validate checks that the given client is valid for usage with the package.
func Validate(client *Client) error {
	if _, err := client.Items(1); err != nil {
		return err
	}

	return nil
}
