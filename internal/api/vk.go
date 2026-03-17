package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nil2x/cheburnet/internal/config"
)

type vkErrorResult1 struct {
	Error vkErrorResponse1 `json:"error"`
}

type vkErrorResponse1 struct {
	ErrorCode int    `json:"error_code"`
	ErrorMsg  string `json:"error_msg"`
}

func (r vkErrorResult1) check() error {
	switch r.Error.ErrorCode {
	case 0:
		return nil
	case 9:
		return errors.New("flood control")
	default:
		return fmt.Errorf("code %d: %s", r.Error.ErrorCode, r.Error.ErrorMsg)
	}
}

type vkErrorResult2 struct {
	Error      string `json:"error"`
	ErrorDescr string `json:"error_descr"`
}

func (r vkErrorResult2) check() error {
	if len(r.Error) > 0 {
		return fmt.Errorf("%v: %v", r.Error, r.ErrorDescr)
	}

	return nil
}

type vkIntResult struct {
	Response int `json:"response"`
}

func (r vkIntResult) check(method string) error {
	if r.Response == 0 {
		return fmt.Errorf("%v: failed", method)
	}

	return nil
}

// VKClient is a wrapper over VK API. See https://dev.vk.com/ru/reference.
//
// Some methods require club credentials, some methods require user credentials,
// some require both. Credentials are mandatory and you should pass all required ones.
//
// Note that VK over time may hide some of its API methods from public.
// The methods remain usable, but have no place in official documentation anymore.
// For this reason some methods of the client will link to unofficial documentation.
type VKClient struct {
	cfg     config.API
	origin  string
	version string
}

func NewVKClient(cfg config.API) *VKClient {
	return &VKClient{
		cfg:     cfg,
		origin:  "https://api.vk.ru",
		version: "5.199",
	}
}

// createURL creates final URL for use in request.
//
// Example of method value:
//
//	"account.getAppPermissions"
func (c *VKClient) createURL(method string, values url.Values) string {
	method = strings.TrimPrefix(method, "/")

	return fmt.Sprintf("%v/method/%v?%s", c.origin, method, values.Encode())
}

type tokenType int

const (
	tokenTypeClub tokenType = iota
	tokenTypeUser
)

// createValues creates mandatory values for use in request.
func (c *VKClient) createValues(token string, tokenType tokenType) (url.Values, error) {
	if tokenType == tokenTypeUser && c.cfg.Unathorized {
		return nil, errors.New("user is unathorized")
	}

	v := url.Values{
		"v":            []string{c.version},
		"access_token": []string{token},
	}

	return v, nil
}

// createForm creates form for use in request.
// Returns form data and Content-Type header value.
func (c *VKClient) createForm(fields map[string]string, files map[string][]byte) (io.Reader, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, "", err
		}
	}

	for k, v := range files {
		field := strings.Split(k, ".")[0]
		fw, err := writer.CreateFormFile(field, k)

		if err != nil {
			return nil, "", err
		}

		if _, err := fw.Write(v); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return body, writer.FormDataContentType(), nil
}

type vkDoParams struct {
	// Value should be the same that was used for request.
	club config.Club

	// Value should be the same that was used for request.
	user config.User

	// Request timeout. If specified, then this value is used instead of client
	// timeout, else client timeout is used.
	timeout time.Duration
}

// do makes HTTP request. Returns response body in case of successful response.
//
// If you sending more than 2000 bytes, then use POST request and form body (createForm).
// Otherwise, use GET request and query string (createValues).
func (c *VKClient) do(req *http.Request, params vkDoParams) ([]byte, error) {
	var timeout time.Duration

	if params.timeout > 0 {
		timeout = params.timeout
	} else {
		timeout = c.cfg.Timeout()
	}

	if timeout > 0 {
		ctx, cancel := context.WithTimeout(req.Context(), timeout)
		defer cancel()

		req = req.WithContext(ctx)
	}

	// descr is used for verbose error message.
	method := strings.TrimPrefix(req.URL.Path, "/method/")
	descr := fmt.Sprintf("(method=%v club=%v user=%v)", method, params.club.Name, params.user.Name)

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		// Trim long and sensitive URL values from error message.
		if e, ok := err.(*url.Error); ok {
			e.URL = req.URL.Path
		}

		return nil, fmt.Errorf("%v %v", err, descr)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %v %v", resp.StatusCode, descr)
	}

	data, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, fmt.Errorf("read: %v %v", err, descr)
	}

	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		// VK have two types of error response.
		var resp1 vkErrorResult1
		var resp2 vkErrorResult2

		var checkErr error

		if err := json.Unmarshal(data, &resp1); err == nil {
			if err := resp1.check(); err != nil {
				checkErr = err
			}
		}

		if err := json.Unmarshal(data, &resp2); err == nil {
			if err := resp2.check(); err != nil {
				checkErr = err
			}
		}

		if checkErr != nil {
			return nil, fmt.Errorf("%v %v", checkErr, descr)
		}
	}

	return data, nil
}

// Download downloads resource at the given uri and returns it as is.
func (c *VKClient) Download(uri string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return nil, err
	}

	data, err := c.do(req, vkDoParams{})

	if err != nil {
		return nil, err
	}

	return data, nil
}

type MessagesSendParams struct {
	Message string
}

type MessagesSendResponse struct {
	ID int
}

// https://dev.vk.com/ru/method/messages.send
func (c *VKClient) MessagesSend(club config.Club, user config.User, params MessagesSendParams) (MessagesSendResponse, error) {
	form := map[string]string{
		"user_id":   user.ID,
		"random_id": "0",
		"message":   params.Message,
	}
	body, ct, err := c.createForm(form, nil)

	if err != nil {
		return MessagesSendResponse{}, err
	}

	values, err := c.createValues(club.AccessToken, tokenTypeClub)

	if err != nil {
		return MessagesSendResponse{}, err
	}

	uri := c.createURL("messages.send", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return MessagesSendResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := c.do(req, vkDoParams{club: club, user: user})

	if err != nil {
		return MessagesSendResponse{}, err
	}

	result := vkIntResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return MessagesSendResponse{}, err
	}

	if err := result.check("messages.send"); err != nil {
		return MessagesSendResponse{}, err
	}

	resp := MessagesSendResponse{
		ID: result.Response,
	}

	return resp, nil
}

type WallPostParams struct {
	Message string
}

type wallPostResult struct {
	Response WallPostResponse `json:"response"`
}

type WallPostResponse struct {
	PostID int `json:"post_id"`
}

// https://dev.vk.com/ru/method/wall.post
func (c *VKClient) WallPost(club config.Club, params WallPostParams) (WallPostResponse, error) {
	form := map[string]string{
		"owner_id": "-" + club.ID,
		"message":  params.Message,
	}
	body, ct, err := c.createForm(form, nil)

	if err != nil {
		return WallPostResponse{}, err
	}

	values, err := c.createValues(club.AccessToken, tokenTypeClub)

	if err != nil {
		return WallPostResponse{}, err
	}

	uri := c.createURL("wall.post", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return WallPostResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := c.do(req, vkDoParams{club: club})

	if err != nil {
		return WallPostResponse{}, err
	}

	result := wallPostResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return WallPostResponse{}, err
	}

	return result.Response, nil
}

type WallCreateCommentParams struct {
	PostID  int
	Message string
}

type wallCreateCommentResult struct {
	Response WallCreateCommentResponse `json:"response"`
}

type WallCreateCommentResponse struct {
	CommentID int `json:"comment_id"`
}

// https://dev.vk.com/ru/method/wall.createComment
func (c *VKClient) WallCreateComment(club config.Club, params WallCreateCommentParams) (WallCreateCommentResponse, error) {
	form := map[string]string{
		"owner_id": "-" + club.ID,
		"post_id":  fmt.Sprint(params.PostID),
		"message":  params.Message,
	}
	body, ct, err := c.createForm(form, nil)

	if err != nil {
		return WallCreateCommentResponse{}, err
	}

	values, err := c.createValues(club.AccessToken, tokenTypeClub)

	if err != nil {
		return WallCreateCommentResponse{}, err
	}

	uri := c.createURL("wall.createComment", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return WallCreateCommentResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := c.do(req, vkDoParams{club: club})

	if err != nil {
		return WallCreateCommentResponse{}, err
	}

	result := wallCreateCommentResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return WallCreateCommentResponse{}, err
	}

	return result.Response, nil
}

type docsGetWallUploadServerResult struct {
	Response DocsGetWallUploadServerResponse `json:"response"`
}

type DocsGetWallUploadServerResponse struct {
	UploadURL string `json:"upload_url"`
}

// https://dev.vk.com/ru/method/docs.getWallUploadServer
func (c *VKClient) DocsGetWallUploadServer(club config.Club) (DocsGetWallUploadServerResponse, error) {
	values, err := c.createValues(club.AccessToken, tokenTypeClub)

	if err != nil {
		return DocsGetWallUploadServerResponse{}, err
	}

	values.Set("group_id", club.ID)

	uri := c.createURL("docs.getWallUploadServer", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return DocsGetWallUploadServerResponse{}, err
	}

	data, err := c.do(req, vkDoParams{club: club})

	if err != nil {
		return DocsGetWallUploadServerResponse{}, err
	}

	result := docsGetWallUploadServerResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return DocsGetWallUploadServerResponse{}, err
	}

	return result.Response, nil
}

type DocsUploadParams struct {
	UploadURL string
	Data      []byte
}

type docsUploadResult struct {
	DocsUploadResponse
}

type DocsUploadResponse struct {
	File string `json:"file"`
}

// https://dev.vk.com/ru/api/upload/document-in-profile
func (c *VKClient) DocsUpload(club config.Club, params DocsUploadParams) (DocsUploadResponse, error) {
	files := map[string][]byte{
		"file.txt": params.Data,
	}
	body, ct, err := c.createForm(nil, files)

	if err != nil {
		return DocsUploadResponse{}, err
	}

	req, err := http.NewRequest(http.MethodPost, params.UploadURL, body)

	if err != nil {
		return DocsUploadResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := c.do(req, vkDoParams{club: club})

	if err != nil {
		return DocsUploadResponse{}, err
	}

	result := docsUploadResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return DocsUploadResponse{}, err
	}

	return result.DocsUploadResponse, nil
}

type DocsSaveParams struct {
	File string
}

type docsSaveResult struct {
	Response DocsSaveResponse `json:"response"`
}

type DocsSaveResponse struct {
	Type string   `json:"type"`
	Doc  Document `json:"doc"`
}

type Document struct {
	ID   int    `json:"id"`
	Size int    `json:"size"`
	URL  string `json:"url"`
}

// https://web.archive.org/web/20220730135346/https://vk.com/dev/docs.save
//
// https://pkg.go.dev/github.com/SevereCloud/vksdk/v3@v3.3.0/api#VK.DocsSave
func (c *VKClient) DocsSave(club config.Club, params DocsSaveParams) (DocsSaveResponse, error) {
	values, err := c.createValues(club.AccessToken, tokenTypeClub)

	if err != nil {
		return DocsSaveResponse{}, err
	}

	values.Set("file", params.File)

	uri := c.createURL("docs.save", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return DocsSaveResponse{}, err
	}

	data, err := c.do(req, vkDoParams{club: club})

	if err != nil {
		return DocsSaveResponse{}, err
	}

	result := docsSaveResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return DocsSaveResponse{}, err
	}

	return result.Response, nil
}

// DocsUploadAndSave combines DocsGetWallUploadServer, DocsUpload and DocsSave.
//
// You should specify only DocsUploadParams.Data.
func (c *VKClient) DocsUploadAndSave(club config.Club, params DocsUploadParams) (DocsSaveResponse, error) {
	server, err := c.DocsGetWallUploadServer(club)

	if err != nil {
		return DocsSaveResponse{}, err
	}

	upload, err := c.DocsUpload(club, DocsUploadParams{
		UploadURL: server.UploadURL,
		Data:      params.Data,
	})

	if err != nil {
		return DocsSaveResponse{}, err
	}

	saved, err := c.DocsSave(club, DocsSaveParams{
		File: upload.File,
	})

	if err != nil {
		return DocsSaveResponse{}, err
	}

	return saved, nil
}

type photosGetUploadServerResult struct {
	Response PhotosGetUploadServerResponse `json:"response"`
}

type PhotosGetUploadServerResponse struct {
	UploadURL string `json:"upload_url"`
}

// https://dev.vk.com/ru/method/photos.getUploadServer
func (c *VKClient) PhotosGetUploadServer(club config.Club, user config.User) (PhotosGetUploadServerResponse, error) {
	values, err := c.createValues(user.AccessToken, tokenTypeUser)

	if err != nil {
		return PhotosGetUploadServerResponse{}, err
	}

	values.Set("group_id", club.ID)
	values.Set("album_id", club.AlbumID)

	uri := c.createURL("photos.getUploadServer", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return PhotosGetUploadServerResponse{}, err
	}

	data, err := c.do(req, vkDoParams{club: club, user: user})

	if err != nil {
		return PhotosGetUploadServerResponse{}, err
	}

	result := photosGetUploadServerResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return PhotosGetUploadServerResponse{}, err
	}

	return result.Response, nil
}

type PhotosUploadParams struct {
	UploadURL string
	Data      []byte
}

type photosUploadResult struct {
	PhotosUploadResponse
}

func (r photosUploadResult) check() error {
	if r.PhotosList == "" || r.PhotosList == "[]" {
		return errors.New("photos.upload: failed")
	}

	return nil
}

type PhotosUploadResponse struct {
	Server     int    `json:"server"`
	PhotosList string `json:"photos_list"`
	Hash       string `json:"hash"`
}

// https://dev.vk.com/ru/api/upload/album-photos
func (c *VKClient) PhotosUpload(club config.Club, user config.User, params PhotosUploadParams) (PhotosUploadResponse, error) {
	files := map[string][]byte{
		"file1.png": params.Data,
	}
	body, ct, err := c.createForm(nil, files)

	if err != nil {
		return PhotosUploadResponse{}, err
	}

	req, err := http.NewRequest(http.MethodPost, params.UploadURL, body)

	if err != nil {
		return PhotosUploadResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := c.do(req, vkDoParams{club: club, user: user})

	if err != nil {
		return PhotosUploadResponse{}, err
	}

	result := photosUploadResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return PhotosUploadResponse{}, err
	}

	if err := result.check(); err != nil {
		return PhotosUploadResponse{}, err
	}

	return result.PhotosUploadResponse, nil
}

type PhotosSaveParams struct {
	PhotosList string
	Server     int
	Hash       string
	Caption    string
}

type photosSaveResult struct {
	Response []PhotosSaveResponse `json:"response"`
}

func (r photosSaveResult) check() error {
	if len(r.Response) == 0 {
		return errors.New("photos.save: failed")
	}

	return nil
}

type PhotosSaveResponse struct {
	ID int `json:"id"`
}

// https://dev.vk.com/ru/method/photos.save
func (c *VKClient) PhotosSave(club config.Club, user config.User, params PhotosSaveParams) (PhotosSaveResponse, error) {
	values, err := c.createValues(user.AccessToken, tokenTypeUser)

	if err != nil {
		return PhotosSaveResponse{}, err
	}

	values.Set("group_id", club.ID)
	values.Set("album_id", club.AlbumID)
	values.Set("photos_list", params.PhotosList)
	values.Set("server", fmt.Sprint(params.Server))
	values.Set("hash", params.Hash)
	values.Set("caption", params.Caption)

	uri := c.createURL("photos.save", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return PhotosSaveResponse{}, err
	}

	data, err := c.do(req, vkDoParams{club: club, user: user})

	if err != nil {
		return PhotosSaveResponse{}, err
	}

	result := photosSaveResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return PhotosSaveResponse{}, err
	}

	if err := result.check(); err != nil {
		return PhotosSaveResponse{}, err
	}

	return result.Response[0], nil
}

type PhotosUploadAndSaveParams struct {
	PhotosUploadParams
	PhotosSaveParams
}

// PhotosUploadAndSave combines PhotosGetUploadServer, PhotosUpload and PhotosSave.
//
// You should specify only PhotosUploadParams.Data abd PhotosSaveParams.Caption.
func (c *VKClient) PhotosUploadAndSave(club config.Club, user config.User, params PhotosUploadAndSaveParams) (PhotosSaveResponse, error) {
	server, err := c.PhotosGetUploadServer(club, user)

	if err != nil {
		return PhotosSaveResponse{}, err
	}

	params.PhotosUploadParams.UploadURL = server.UploadURL
	upload, err := c.PhotosUpload(club, user, params.PhotosUploadParams)

	if err != nil {
		return PhotosSaveResponse{}, err
	}

	params.PhotosSaveParams.PhotosList = upload.PhotosList
	params.PhotosSaveParams.Server = upload.Server
	params.PhotosSaveParams.Hash = upload.Hash
	saved, err := c.PhotosSave(club, user, params.PhotosSaveParams)

	if err != nil {
		return PhotosSaveResponse{}, err
	}

	return saved, nil
}

type PhotosCreateCommentParams struct {
	Message string
}

type PhotosCreateCommentResponse struct {
	ID int
}

// https://dev.vk.com/ru/method/photos.createComment
func (c *VKClient) PhotosCreateComment(club config.Club, user config.User, params PhotosCreateCommentParams) (PhotosCreateCommentResponse, error) {
	form := map[string]string{
		"owner_id": "-" + club.ID,
		"photo_id": club.PhotoID,
		"message":  params.Message,
	}
	body, ct, err := c.createForm(form, nil)

	if err != nil {
		return PhotosCreateCommentResponse{}, err
	}

	values, err := c.createValues(user.AccessToken, tokenTypeUser)

	if err != nil {
		return PhotosCreateCommentResponse{}, err
	}

	uri := c.createURL("photos.createComment", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return PhotosCreateCommentResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := c.do(req, vkDoParams{club: club, user: user})

	if err != nil {
		return PhotosCreateCommentResponse{}, err
	}

	result := vkIntResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return PhotosCreateCommentResponse{}, err
	}

	if err := result.check("photos.createComment"); err != nil {
		return PhotosCreateCommentResponse{}, err
	}

	resp := PhotosCreateCommentResponse{
		ID: result.Response,
	}

	return resp, nil
}

type VideoCreateCommentParams struct {
	Message string
}

type VideoCreateCommentResponse struct {
	ID int
}

// https://dev.vk.com/ru/method/video.createComment
func (c *VKClient) VideoCreateComment(club config.Club, user config.User, params VideoCreateCommentParams) (VideoCreateCommentResponse, error) {
	form := map[string]string{
		"owner_id": "-" + club.ID,
		"video_id": club.VideoID,
		"message":  params.Message,
	}
	body, ct, err := c.createForm(form, nil)

	if err != nil {
		return VideoCreateCommentResponse{}, err
	}

	values, err := c.createValues(user.AccessToken, tokenTypeUser)

	if err != nil {
		return VideoCreateCommentResponse{}, err
	}

	uri := c.createURL("video.createComment", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return VideoCreateCommentResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := c.do(req, vkDoParams{club: club, user: user})

	if err != nil {
		return VideoCreateCommentResponse{}, err
	}

	result := vkIntResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return VideoCreateCommentResponse{}, err
	}

	if err := result.check("video.createComment"); err != nil {
		return VideoCreateCommentResponse{}, err
	}

	resp := VideoCreateCommentResponse{
		ID: result.Response,
	}

	return resp, nil
}

type MarketCreateCommentParams struct {
	Message string
}

type MarketCreateCommentResponse struct {
	ID int
}

// https://dev.vk.com/ru/method/market.createComment
func (c *VKClient) MarketCreateComment(club config.Club, user config.User, params MarketCreateCommentParams) (MarketCreateCommentResponse, error) {
	form := map[string]string{
		"owner_id": "-" + club.ID,
		"item_id":  club.MarketID,
		"message":  params.Message,
	}
	body, ct, err := c.createForm(form, nil)

	if err != nil {
		return MarketCreateCommentResponse{}, err
	}

	values, err := c.createValues(user.AccessToken, tokenTypeUser)

	if err != nil {
		return MarketCreateCommentResponse{}, err
	}

	uri := c.createURL("market.createComment", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return MarketCreateCommentResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := c.do(req, vkDoParams{club: club, user: user})

	if err != nil {
		return MarketCreateCommentResponse{}, err
	}

	result := vkIntResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return MarketCreateCommentResponse{}, err
	}

	if err := result.check("market.createComment"); err != nil {
		return MarketCreateCommentResponse{}, err
	}

	resp := MarketCreateCommentResponse{
		ID: result.Response,
	}

	return resp, nil
}

type BoardAddTopicParams struct {
	Title string
	Text  string
}

type BoardAddTopicResponse struct {
	ID int
}

// https://web.archive.org/web/20230317104930/https://vk.com/dev/board.addTopic
//
// https://pkg.go.dev/github.com/SevereCloud/vksdk/v3@v3.3.0/api#VK.BoardAddTopic
func (c *VKClient) BoardAddTopic(club config.Club, user config.User, params BoardAddTopicParams) (BoardAddTopicResponse, error) {
	form := map[string]string{
		"group_id": club.ID,
		"title":    params.Title,
		"text":     params.Text,
	}
	body, ct, err := c.createForm(form, nil)

	if err != nil {
		return BoardAddTopicResponse{}, err
	}

	values, err := c.createValues(user.AccessToken, tokenTypeUser)

	if err != nil {
		return BoardAddTopicResponse{}, err
	}

	uri := c.createURL("board.addTopic", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return BoardAddTopicResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := c.do(req, vkDoParams{club: club, user: user})

	if err != nil {
		return BoardAddTopicResponse{}, err
	}

	result := vkIntResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return BoardAddTopicResponse{}, err
	}

	if err := result.check("board.addTopic"); err != nil {
		return BoardAddTopicResponse{}, err
	}

	resp := BoardAddTopicResponse{
		ID: result.Response,
	}

	return resp, nil
}

type BoardCreateCommentParams struct {
	TopicID int
	Message string
}

type BoardCreateCommentResponse struct {
	ID int
}

// https://web.archive.org/web/20220203210531/https://vk.com/dev/board.createComment
//
// https://pkg.go.dev/github.com/SevereCloud/vksdk/v3@v3.3.0/api#VK.BoardCreateComment
func (c *VKClient) BoardCreateComment(club config.Club, user config.User, params BoardCreateCommentParams) (BoardCreateCommentResponse, error) {
	form := map[string]string{
		"group_id": club.ID,
		"topic_id": fmt.Sprint(params.TopicID),
		"message":  params.Message,
	}
	body, ct, err := c.createForm(form, nil)

	if err != nil {
		return BoardCreateCommentResponse{}, err
	}

	values, err := c.createValues(user.AccessToken, tokenTypeUser)

	if err != nil {
		return BoardCreateCommentResponse{}, err
	}

	uri := c.createURL("board.createComment", values)
	req, err := http.NewRequest(http.MethodPost, uri, body)

	if err != nil {
		return BoardCreateCommentResponse{}, err
	}

	req.Header.Set("Content-Type", ct)

	data, err := c.do(req, vkDoParams{club: club, user: user})

	if err != nil {
		return BoardCreateCommentResponse{}, err
	}

	result := vkIntResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return BoardCreateCommentResponse{}, err
	}

	if err := result.check("board.createComment"); err != nil {
		return BoardCreateCommentResponse{}, err
	}

	resp := BoardCreateCommentResponse{
		ID: result.Response,
	}

	return resp, nil
}

type StorageGetParams struct {
	Keys []string
}

type storageGetResult struct {
	Response []StorageGetResponse `json:"response"`
}

type StorageGetResponse struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// https://dev.vk.com/ru/method/storage.get
func (c *VKClient) StorageGet(club config.Club, params StorageGetParams) ([]StorageGetResponse, error) {
	values, err := c.createValues(club.AccessToken, tokenTypeClub)

	if err != nil {
		return nil, err
	}

	values.Set("keys", strings.Join(params.Keys, ","))
	values.Set("user_id", club.ID)

	uri := c.createURL("storage.get", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return nil, err
	}

	data, err := c.do(req, vkDoParams{club: club})

	if err != nil {
		return nil, err
	}

	result := storageGetResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result.Response, nil
}

type StorageSetParams struct {
	Key   string
	Value string
}

// https://dev.vk.com/ru/method/storage.set
func (c *VKClient) StorageSet(club config.Club, params StorageSetParams) error {
	values, err := c.createValues(club.AccessToken, tokenTypeClub)

	if err != nil {
		return err
	}

	values.Set("key", params.Key)
	values.Set("value", params.Value)
	values.Set("user_id", club.ID)

	uri := c.createURL("storage.set", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return err
	}

	data, err := c.do(req, vkDoParams{club: club})

	if err != nil {
		return err
	}

	result := vkIntResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}

	if err := result.check("storage.set"); err != nil {
		return err
	}

	return nil
}

type groupsGetLongPollServerResult struct {
	Response GroupsGetLongPollServerResponse `json:"response"`
}

func (r groupsGetLongPollServerResult) check() error {
	if len(r.Response.Key) == 0 {
		return errors.New("key is empty")
	}

	if len(r.Response.Server) == 0 {
		return errors.New("server is empty")
	}

	return nil
}

type GroupsGetLongPollServerResponse struct {
	Key    string      `json:"key"`
	Server string      `json:"server"`
	TS     json.Number `json:"ts"`
}

// https://dev.vk.com/ru/method/groups.getLongPollServer
func (c *VKClient) GroupsGetLongPollServer(club config.Club) (GroupsGetLongPollServerResponse, error) {
	values, err := c.createValues(club.AccessToken, tokenTypeClub)

	if err != nil {
		return GroupsGetLongPollServerResponse{}, err
	}

	values.Set("group_id", club.ID)

	uri := c.createURL("groups.getLongPollServer", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return GroupsGetLongPollServerResponse{}, err
	}

	data, err := c.do(req, vkDoParams{club: club})

	if err != nil {
		return GroupsGetLongPollServerResponse{}, err
	}

	result := groupsGetLongPollServerResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return GroupsGetLongPollServerResponse{}, err
	}

	if err := result.check(); err != nil {
		return GroupsGetLongPollServerResponse{}, err
	}

	return result.Response, nil
}

type GroupsUseLongPollServerResponse struct {
	Failed  int         `json:"failed"`
	TS      json.Number `json:"ts"`
	Updates []Update    `json:"updates"`
}

// https://dev.vk.com/ru/api/community-events/json-schema
type Update struct {
	Type    string       `json:"type"`
	EventID string       `json:"event_id"`
	V       string       `json:"v"`
	GroupID int          `json:"group_id"`
	Object  UpdateObject `json:"object"`
}

type UpdateType int

const (
	UpdateTypeUnknown UpdateType = iota
	UpdateTypeMessageReply
	UpdateTypeWallPostNew
	UpdateTypeWallReplyNew
	UpdateTypePhotoNew
	UpdateTypeGroupChangeSettings
	UpdateTypeVideoCommentNew
	UpdateTypePhotoCommentNew
	UpdateTypeMarketCommentNew
	UpdateTypeBoardPostNew
)

var supportedUpdateType = []string{
	"message_reply",
	"wall_post_new",
	"wall_reply_new",
	"photo_new",
	"group_change_settings",
	"video_comment_new",
	"photo_comment_new",
	"market_comment_new",
	"board_post_new",
}

func (u Update) TypeEnum() UpdateType {
	switch u.Type {
	case "message_reply":
		return UpdateTypeMessageReply
	case "wall_post_new":
		return UpdateTypeWallPostNew
	case "wall_reply_new":
		return UpdateTypeWallReplyNew
	case "photo_new":
		return UpdateTypePhotoNew
	case "group_change_settings":
		return UpdateTypeGroupChangeSettings
	case "video_comment_new":
		return UpdateTypeVideoCommentNew
	case "photo_comment_new":
		return UpdateTypePhotoCommentNew
	case "market_comment_new":
		return UpdateTypeMarketCommentNew
	case "board_post_new":
		return UpdateTypeBoardPostNew
	default:
		return UpdateTypeUnknown
	}
}

type UpdateObject struct {
	ID        int           `json:"id"`
	Date      int           `json:"date"`
	Text      string        `json:"text"`
	OrigPhoto UpdatePhoto   `json:"orig_photo"`
	Changes   UpdateChanges `json:"changes"`
}

type UpdatePhoto struct {
	URL string `json:"url"`
}

type UpdateChanges struct {
	Description UpdateChangeString `json:"description"`
	Website     UpdateChangeString `json:"website"`
}

type UpdateChangeString struct {
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
}

// https://dev.vk.com/ru/api/bots-long-poll/getting-started
func (c *VKClient) GroupsUseLongPollServer(ctx context.Context, server GroupsGetLongPollServerResponse, last GroupsUseLongPollServerResponse) (GroupsUseLongPollServerResponse, error) {
	values := url.Values{}

	values.Set("act", "a_check")
	values.Set("key", server.Key)
	values.Set("ts", last.TS.String())
	values.Set("wait", "25")

	uri := fmt.Sprintf("%v?%v", server.Server, values.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)

	if err != nil {
		return GroupsUseLongPollServerResponse{}, err
	}

	data, err := c.do(req, vkDoParams{timeout: 30 * time.Second})

	if err != nil {
		return GroupsUseLongPollServerResponse{}, err
	}

	result := GroupsUseLongPollServerResponse{}

	if err := json.Unmarshal(data, &result); err != nil {
		return GroupsUseLongPollServerResponse{}, err
	}

	return result, nil
}

type groupsGetLongPollSettingsResult struct {
	Response GroupsGetLongPollSettingsResponse `json:"response"`
}

type GroupsGetLongPollSettingsResponse struct {
	IsEnabled bool           `json:"is_enabled"`
	Events    map[string]int `json:"events"`
}

// https://dev.vk.com/ru/method/groups.getLongPollSettings
func (c *VKClient) GroupsGetLongPollSettings(club config.Club) (GroupsGetLongPollSettingsResponse, error) {
	values, err := c.createValues(club.AccessToken, tokenTypeClub)

	if err != nil {
		return GroupsGetLongPollSettingsResponse{}, err
	}

	values.Set("group_id", club.ID)

	uri := c.createURL("groups.getLongPollSettings", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return GroupsGetLongPollSettingsResponse{}, err
	}

	data, err := c.do(req, vkDoParams{club: club})

	if err != nil {
		return GroupsGetLongPollSettingsResponse{}, err
	}

	result := groupsGetLongPollSettingsResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return GroupsGetLongPollSettingsResponse{}, err
	}

	return result.Response, nil
}

type groupsGetTokenPermissionsResult struct {
	Response GroupsGetTokenPermissionsResponse `json:"response"`
}

type GroupsGetTokenPermissionsResponse struct {
	Mask int `json:"mask"`
}

// https://dev.vk.com/ru/method/groups.getTokenPermissions
func (c *VKClient) GroupsGetTokenPermissions(club config.Club) (GroupsGetTokenPermissionsResponse, error) {
	values, err := c.createValues(club.AccessToken, tokenTypeClub)

	if err != nil {
		return GroupsGetTokenPermissionsResponse{}, err
	}

	uri := c.createURL("groups.getTokenPermissions", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return GroupsGetTokenPermissionsResponse{}, err
	}

	data, err := c.do(req, vkDoParams{club: club})

	if err != nil {
		return GroupsGetTokenPermissionsResponse{}, err
	}

	result := groupsGetTokenPermissionsResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return GroupsGetTokenPermissionsResponse{}, err
	}

	return result.Response, nil
}

type GroupsEditParams struct {
	Description string
	Website     string
}

// https://web.archive.org/web/20220428234931/https://vk.com/dev/groups.edit
//
// https://pkg.go.dev/github.com/SevereCloud/vksdk/v3@v3.3.0/api#VK.GroupsEdit
func (c *VKClient) GroupsEdit(club config.Club, params GroupsEditParams) error {
	values, err := c.createValues(club.AccessToken, tokenTypeClub)

	if err != nil {
		return err
	}

	values.Set("group_id", club.ID)

	if len(params.Description) > 0 {
		values.Set("description", params.Description)
	}

	if len(params.Website) > 0 {
		values.Set("website", params.Website)
	}

	uri := c.createURL("groups.edit", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return err
	}

	data, err := c.do(req, vkDoParams{club: club})

	if err != nil {
		return err
	}

	result := vkIntResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}

	if err := result.check("groups.edit"); err != nil {
		return err
	}

	return nil
}

type AccountGetAppPermissionsResponse struct {
	Mask int
}

// https://dev.vk.com/ru/method/account.getAppPermissions
func (c *VKClient) AccountGetAppPermissions(user config.User) (AccountGetAppPermissionsResponse, error) {
	values, err := c.createValues(user.AccessToken, tokenTypeUser)

	if err != nil {
		return AccountGetAppPermissionsResponse{}, err
	}

	uri := c.createURL("account.getAppPermissions", values)
	req, err := http.NewRequest(http.MethodGet, uri, nil)

	if err != nil {
		return AccountGetAppPermissionsResponse{}, err
	}

	data, err := c.do(req, vkDoParams{user: user})

	if err != nil {
		return AccountGetAppPermissionsResponse{}, err
	}

	result := vkIntResult{}

	if err := json.Unmarshal(data, &result); err != nil {
		return AccountGetAppPermissionsResponse{}, err
	}

	resp := AccountGetAppPermissionsResponse{
		Mask: result.Response,
	}

	return resp, nil
}
