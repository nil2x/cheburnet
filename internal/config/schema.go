package config

import (
	"fmt"
	"net"
	"time"
)

// Config holds configuration of program packages.
// See README for description of some of the fields.
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
	SocksPort uint16 `env:"SOCKS_PORT"`
}
