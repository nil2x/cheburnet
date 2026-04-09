package ok

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/nil2x/cheburnet/internal/api"
	"github.com/nil2x/cheburnet/internal/config"
	"github.com/nil2x/cheburnet/internal/transform"
)

type statusResponse struct {
	Success bool `json:"success"`
}

func (r statusResponse) check() error {
	if !r.Success {
		return errors.New("failed")
	}

	return nil
}

type errorResponse struct {
	ErrorCode int    `json:"error_code"`
	ErrorMsg  string `json:"error_msg"`
}

func (r errorResponse) check() error {
	switch r.ErrorCode {
	case 0:
		return nil
	case 8:
		return errors.New("flood control")
	default:
		return fmt.Errorf("code %d: %s", r.ErrorCode, r.ErrorMsg)
	}
}

// Client is a client for interaction with Odnoklassniki.
type Client struct {
	Name     string
	StorageC *api.StorageClient
	cfgAPI   config.API
	cfgOK    config.OK
}

// New returns new Client for the given config.
func New(cfgAPI config.API, cfgOK config.OK) *Client {
	if cfgOK.Origin == "" {
		cfgOK.Origin = "https://api.ok.ru"
	}

	c := &Client{
		Name:     cfgOK.Name,
		StorageC: api.NewStorageClient(),
		cfgAPI:   cfgAPI,
		cfgOK:    cfgOK,
	}

	return c
}

func (c *Client) createURL(values url.Values) string {
	return fmt.Sprintf("%v/fb.do?%s", c.cfgOK.Origin, values.Encode())
}

func (c *Client) createValues(method string) url.Values {
	v := url.Values{
		"application_key": []string{c.cfgOK.PublicKey},
		"format":          []string{"JSON"},
		"method":          []string{method},
	}

	return v
}

// addSignature adds signature query argument to the given values.
//
// See https://apiok.ru/dev/methods#signature_calc
func (c *Client) addSignature(v url.Values) {
	clone := url.Values(http.Header(v).Clone())

	var session_secret_key string

	if s := clone.Get("access_token"); s == "" {
		session_secret_key = c.cfgOK.SecretKey
	} else {
		session_secret_key = transform.BytesToMD5([]byte(s + c.cfgOK.SecretKey))
	}

	clone.Del("session_key")
	clone.Del("access_token")

	params := clone.Encode()
	params = strings.ReplaceAll(params, "&", "")
	params, _ = url.QueryUnescape(params)

	sig := transform.BytesToMD5([]byte(params + session_secret_key))

	v.Add("sig", sig)
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

	descr := fmt.Sprintf("(name=%v method=%v)", c.cfgOK.Name, req.URL.Query().Get("method"))
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

	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		var resp errorResponse
		var checkErr error

		if err := json.Unmarshal(data, &resp); err == nil {
			if err := resp.check(); err != nil {
				checkErr = err
			}
		}

		if checkErr != nil {
			return nil, fmt.Errorf("%v %v", checkErr, descr)
		}
	}

	return data, nil
}

type StorageGetParams struct {
	Keys []string
}

type StorageGetResponse struct {
	Data map[string]string `json:"data"`
}

func (r StorageGetResponse) Convert() []api.StorageGetResponse {
	var result []api.StorageGetResponse

	for key, value := range r.Data {
		result = append(result, api.StorageGetResponse{
			Key:   key,
			Value: value,
		})
	}

	return result
}

// https://apiok.ru/dev/methods/rest/storage/storage.get
func (c *Client) StorageGet(params StorageGetParams) (StorageGetResponse, error) {
	values := c.createValues("storage.get")

	values.Set("keys", strings.Join(params.Keys, ","))
	c.addSignature(values)

	uri := c.createURL(values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return StorageGetResponse{}, err
	}

	data, err := c.do(req)

	if err != nil {
		return StorageGetResponse{}, err
	}

	result := StorageGetResponse{}

	if err := json.Unmarshal(data, &result); err != nil {
		return StorageGetResponse{}, err
	}

	return result, nil
}

type StorageSetParams struct {
	Key   string
	Value string
}

// https://apiok.ru/dev/methods/rest/storage/storage.set
func (c *Client) StorageSet(params StorageSetParams) error {
	values := c.createValues("storage.set")

	values.Set("key", params.Key)
	values.Set("value", params.Value)
	c.addSignature(values)

	uri := c.createURL(values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return err
	}

	data, err := c.do(req)

	if err != nil {
		return err
	}

	resp := statusResponse{}

	if err := json.Unmarshal(data, &resp); err != nil {
		return err
	}

	if err := resp.check(); err != nil {
		return err
	}

	return nil
}
