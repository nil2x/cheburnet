package config

import (
	"fmt"
	"runtime"
)

// Will be embedded at build time.
var (
	version string
	commit  string
)

// BuildInfo returns build information string that can be used for version printing.
func BuildInfo() string {
	v := version
	c := commit

	if v == "" {
		v = "?"
	}

	if c == "" {
		c = "?"
	}

	return fmt.Sprintf("%v %v %v/%v", v, c, runtime.GOOS, runtime.GOARCH)
}

func defaultConfig() Config {
	return Config{
		Log: Log{
			Level:    0,
			Output:   "",
			Truncate: false,
			Payload:  false,
		},
		DNS: DNS{
			Address: Address{
				Host: "",
				Port: 0,
			},
			Provider:  "",
			TimeoutMS: 3 * 1000,
		},
		Session: Session{
			TimeoutMS:            30 * 1000,
			Secret:               "",
			UploadAttempts:       3,
			MuxIntervalMS:        500,
			MethodsEnabled:       map[int]bool{},
			MethodsMaxLenEncoded: map[int]int{},
		},
		Handler: Handler{
			ConnectTimeoutMS: 7 * 1000,
			RetryIntervalMS:  10 * 1000,
			RetryAttempts:    2,
			DownloadAttempts: 3,
		},
		Socks: Socks{
			Address: Address{
				Host: "127.0.0.1",
				Port: 1080,
			},
			AcceptRate:        0,
			ReadSize:          8 * 1024,
			ReadTimeoutMS:     0,
			ReadRate:          1 * 1024 * 1024,
			WriteTimeoutMS:    7 * 1000,
			ForwardSize:       1 * 1024 * 1024,
			ForwardIntervalMS: 500,
		},
		API: API{
			TimeoutMS:      5 * 1000,
			Unathorized:    false,
			UserAgent:      "",
			SkipValidation: false,
		},
		QR: QR{
			ZBarPath:   "zbarimg",
			ImageSize:  512,
			ImageLevel: 1,
			SaveDir:    "",
		},
		Clubs:  []Club{},
		Users:  []User{},
		IMAP:   []IMAP{},
		YaDisk: []YaDisk{},
	}
}
