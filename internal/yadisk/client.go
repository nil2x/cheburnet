package yadisk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/nil2x/cheburnet/internal/config"
)

// Client is a client for interaction with Yandex Disk.
type Client struct {
	Name   string
	cfgAPI config.API
	cfgYa  config.YaDisk
}

// New returns new Client for the given config.
func New(cfgAPI config.API, cfgYa config.YaDisk) *Client {
	if cfgYa.Origin == "" {
		cfgYa.Origin = "https://cloud-api.yandex.net"
	}

	if cfgYa.Root == "" {
		cfgYa.Root = "app:/"
	}

	c := &Client{
		Name:   cfgYa.Name,
		cfgAPI: cfgAPI,
		cfgYa:  cfgYa,
	}

	return c
}

// do is a general method to perform HTTP request.
func (c *Client) do(req *http.Request) ([]byte, error) {
	var timeout = c.cfgAPI.Timeout()

	if timeout > 0 {
		ctx, cancel := context.WithTimeout(req.Context(), timeout)
		defer cancel()

		req = req.WithContext(ctx)
	}

	if c.cfgAPI.UserAgent != "" {
		req.Header.Set("User-Agent", c.cfgAPI.UserAgent)
	}

	req.Header.Set("Authorization", "OAuth "+c.cfgYa.Token)

	descr := fmt.Sprintf("(name=%v method=%v)", c.cfgYa.Name, req.URL.Path)
	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		// Trim long and sensitive URL values from error message.
		if e, ok := err.(*url.Error); ok {
			e.URL = req.URL.Path
		}

		return nil, fmt.Errorf("%v %v", err, descr)
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %v %v", resp.StatusCode, descr)
	}

	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, fmt.Errorf("read: %v %v", err, descr)
	}

	return data, nil
}

type hrefResp struct {
	Href      string `json:"href"`
	Method    string `json:"method"`
	Templated bool   `json:"templated"`
}

func (r hrefResp) check() error {
	if r.Templated {
		return errors.New("templated href is not supported")
	}

	return nil
}

// Upload uploads data in the root directory and returns name of a created file.
//
// ext specifies file extension. Optional. Example: "txt".
//
// See https://yandex.ru/dev/disk-api/doc/ru/reference/upload
func (c *Client) Upload(b []byte, ext string) (string, error) {
	name := uuid.NewString()

	if ext != "" {
		name += "." + ext
	}

	path := path.Join(c.cfgYa.Root, name)

	values := url.Values{}
	values.Set("path", path)
	values.Set("overwrite", "false")

	query := values.Encode()
	url := fmt.Sprintf("%v/v1/disk/resources/upload?%v", c.cfgYa.Origin, query)

	req, err := http.NewRequest(http.MethodGet, url, nil)

	if err != nil {
		return "", err
	}

	data, err := c.do(req)

	if err != nil {
		return "", err
	}

	var resp hrefResp

	if err := json.Unmarshal(data, &resp); err != nil {
		return "", err
	}

	if err := resp.check(); err != nil {
		return "", err
	}

	req, err = http.NewRequest(resp.Method, resp.Href, bytes.NewReader(b))

	if err != nil {
		return "", err
	}

	_, err = c.do(req)

	if err != nil {
		return "", err
	}

	return name, nil
}

// Download downloads file with the given name and returns its content.
//
// See https://yandex.ru/dev/disk-api/doc/ru/reference/content
func (c *Client) Download(name string) ([]byte, error) {
	path := path.Join(c.cfgYa.Root, name)

	values := url.Values{}
	values.Set("path", path)

	query := values.Encode()
	url := fmt.Sprintf("%v/v1/disk/resources/download?%v", c.cfgYa.Origin, query)

	req, err := http.NewRequest(http.MethodGet, url, nil)

	if err != nil {
		return nil, err
	}

	data, err := c.do(req)

	if err != nil {
		return nil, err
	}

	var resp hrefResp

	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	if err := resp.check(); err != nil {
		return nil, err
	}

	req, err = http.NewRequest(resp.Method, resp.Href, nil)

	if err != nil {
		return nil, err
	}

	data, err = c.do(req)

	if err != nil {
		return nil, err
	}

	return data, nil
}

type ItemsResp struct {
	Data ItemsData `json:"_embedded"`
}

type ItemsData struct {
	Items []Item `json:"items"`
}

type Item struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created"`
}

// Items returns files list in the root directory.
// Sorted by creation time from newest to oldest.
//
// limit specifies number of files to return.
//
// See https://yandex.ru/dev/disk-api/doc/ru/reference/meta
func (c *Client) Items(limit int) (ItemsResp, error) {
	values := url.Values{}
	values.Set("path", c.cfgYa.Root)
	values.Set("fields", "_embedded.items.name,_embedded.items.created")
	values.Set("limit", fmt.Sprint(limit))
	values.Set("sort", "-created")

	query := values.Encode()
	url := fmt.Sprintf("%v/v1/disk/resources?%v", c.cfgYa.Origin, query)

	req, err := http.NewRequest(http.MethodGet, url, nil)

	if err != nil {
		return ItemsResp{}, err
	}

	data, err := c.do(req)

	if err != nil {
		return ItemsResp{}, err
	}

	var resp ItemsResp

	if err := json.Unmarshal(data, &resp); err != nil {
		return ItemsResp{}, err
	}

	return resp, nil
}
