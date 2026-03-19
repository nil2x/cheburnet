package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"

	"github.com/nil2x/cheburnet/internal/api"
	"github.com/nil2x/cheburnet/internal/config"
	"github.com/nil2x/cheburnet/internal/handler"
	"github.com/nil2x/cheburnet/internal/session"
	"github.com/nil2x/cheburnet/internal/socks"
	"github.com/nil2x/cheburnet/internal/transform"
)

// Run starts the program, waits its completion and exits with appropriate code.
func Run() {
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 100)
	code := 0

	go func() {
		for err := range errs {
			fmt.Fprintln(os.Stderr, err)
			code = 1
			cancel()
		}
	}()

	if err := run(ctx, errs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}

	close(errs)

	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stdout, "\nPress Enter to exit...")
		fmt.Scanln()
	}

	os.Exit(code)
}

func run(ctx context.Context, errs chan<- error) error {
	flags := config.ParseFlags()

	if flags.PrintVersion {
		fmt.Fprintln(os.Stdout, config.BuildInfo())

		return nil
	}

	if flags.GenerateSecret {
		secret, err := transform.GenerateSecret()

		if err != nil {
			return fmt.Errorf("generate secret: %v", err)
		}

		fmt.Fprintln(os.Stdout, secret)

		return nil
	}

	cfg, err := config.Parse(flags.ConfigPath)

	if err != nil {
		return fmt.Errorf("parse config: %v", err)
	}

	if err := transform.Init(&cfg); err != nil {
		return fmt.Errorf("init encoding: %v", err)
	}

	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("validate config: %v", err)
	}

	if err := config.SetupLog(cfg.Log); err != nil {
		return fmt.Errorf("setup log: %v", err)
	}

	if err := config.SetupDNS(cfg.DNS); err != nil {
		return fmt.Errorf("setup dns: %v", err)
	}

	if err := transform.ValidateQR(cfg.QR); err != nil {
		return fmt.Errorf("validate qr: %v", err)
	}

	if err := session.Init(cfg); err != nil {
		return fmt.Errorf("init session: %v", err)
	}

	vkClient := api.NewVKClient(cfg.API)
	storageClient := api.NewStorageClient()

	for _, club := range cfg.Clubs {
		if err := api.ValidateClub(vkClient, club); err != nil {
			return fmt.Errorf("validate club: %v: %v", club.Name, err)
		}

		if err := api.ValidateLongPoll(vkClient, club); err != nil {
			return fmt.Errorf("validate long poll: %v: %v", club.Name, err)
		}
	}

	for _, user := range cfg.Users {
		if err := api.ValidateUser(vkClient, user); err != nil {
			return fmt.Errorf("validate user: %v: %v", user.Name, err)
		}
	}

	var wg sync.WaitGroup

	if cfg.Socks.Port > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := socks.Listen(ctx, cfg, vkClient, storageClient); err != nil {
				errs <- fmt.Errorf("listen socks: %v", err)
			}
		}()
	}

	for _, club := range cfg.Clubs {
		wg.Add(1)
		go func(club config.Club) {
			defer wg.Done()

			if err := handler.ListenLongPoll(ctx, cfg, vkClient, storageClient, club); err != nil {
				errs <- fmt.Errorf("listen long poll: %v", err)
			}
		}(club)

		wg.Add(1)
		go func(club config.Club) {
			defer wg.Done()

			if err := handler.ListenStorage(ctx, cfg, vkClient, storageClient, club); err != nil {
				errs <- fmt.Errorf("listen storage: %v", err)
			}
		}(club)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := handler.Clear(ctx); err != nil {
			errs <- fmt.Errorf("clear handler: %v", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		if err := session.Clear(ctx, cfg.Session); err != nil {
			errs <- fmt.Errorf("clear session: %v", err)
		}
	}()

	wg.Wait()

	return nil
}
