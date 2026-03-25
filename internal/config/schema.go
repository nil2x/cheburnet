package config

import (
	"fmt"
	"net"
	"time"
)

// Config holds configuration of the program.
// See README for description of some of the fields.
type Config struct {
	Log     Log     `json:"log"`
	DNS     DNS     `json:"dns"`
	Session Session `json:"session"`
	Handler Handler `json:"handler"`
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
	Provider  string `json:"provider"`
	TimeoutMS int    `json:"timeout"`
}

func (cfg DNS) Timeout() time.Duration {
	return time.Duration(cfg.TimeoutMS) * time.Millisecond
}

type Session struct {
	TimeoutMS            int          `json:"timeout"`
	Secret               string       `json:"secret"`
	SecretKey            []byte       `json:"-"`
	UploadAttempts       int          `json:"uploadAttempts"`
	MuxIntervalMS        int          `json:"muxInterval"`
	MethodsEnabled       map[int]bool `json:"methodsEnabled"`
	MethodsMaxLenEncoded map[int]int  `json:"methodsMaxLenEncoded"`
}

func (cfg Session) Timeout() time.Duration {
	return time.Duration(cfg.TimeoutMS) * time.Millisecond
}

func (cfg Session) MuxInterval() time.Duration {
	return time.Duration(cfg.MuxIntervalMS) * time.Millisecond
}

type Handler struct {
	ConnectTimeoutMS int `json:"connectTimeout"`
	RetryIntervalMS  int `json:"retryInterval"`
	RetryAttempts    int `json:"retryAttempts"`
	DownloadAttempts int `json:"downloadAttempts"`
}

func (cfg Handler) ConnectTimeout() time.Duration {
	return time.Duration(cfg.ConnectTimeoutMS) * time.Millisecond
}

func (cfg Handler) RetryInterval() time.Duration {
	return time.Duration(cfg.RetryIntervalMS) * time.Millisecond
}

type Socks struct {
	Address
	AcceptRate        int `json:"acceptRate"`
	ReadSize          int `json:"readSize"`
	ReadTimeoutMS     int `json:"readTimeout"`
	ReadRate          int `json:"readRate"`
	WriteTimeoutMS    int `json:"writeTimeout"`
	ForwardSize       int `json:"forwardSize"`
	ForwardIntervalMS int `json:"forwardInterval"`
}

func (cfg Socks) ReadTimeout() time.Duration {
	return time.Duration(cfg.ReadTimeoutMS) * time.Millisecond
}

func (cfg Socks) WriteTimeout() time.Duration {
	return time.Duration(cfg.WriteTimeoutMS) * time.Millisecond
}

func (cfg Socks) ForwardInterval() time.Duration {
	return time.Duration(cfg.ForwardIntervalMS) * time.Millisecond
}

type API struct {
	TimeoutMS   int    `json:"timeout"`
	Unathorized bool   `json:"unathorized"`
	UserAgent   string `json:"userAgent"`
}

func (cfg API) Timeout() time.Duration {
	return time.Duration(cfg.TimeoutMS) * time.Millisecond
}

type QR struct {
	ZBarPath   string `json:"zbarPath"`
	ImageSize  int    `json:"imageSize"`
	ImageLevel int    `json:"imageLevel"`
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

type Address struct {
	Host string `json:"host"`
	Port uint16 `json:"port"`
}

func (a Address) String() string {
	return net.JoinHostPort(a.Host, fmt.Sprint(a.Port))
}

type Flags struct {
	ConfigPath     string `flag:"config"`
	PrintVersion   bool   `flag:"version"`
	GenerateSecret bool   `flag:"secret"`
}

type Env struct {
	LogOutput string `env:"LOG_OUTPUT"`
	SocksHost string `env:"SOCKS_HOST"`
	SocksPort uint16 `env:"SOCKS_PORT"`
}
