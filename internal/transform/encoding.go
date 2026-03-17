package transform

import (
	"github.com/nil2x/cheburnet/internal/config"
)

// Init should be called at the program start to complete package initialization.
//
// cfg is a parsed config but not yet validated. Init will modify it.
func Init(cfg *config.Config) error {
	if len(cfg.Session.Secret) > 0 {
		key, err := SecretToKey(cfg.Session.Secret)

		if err != nil {
			return err
		}

		cfg.Session.SecretKey = key
	}

	return nil
}
