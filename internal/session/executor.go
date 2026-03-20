package session

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/nil2x/cheburnet/internal/api"
	"github.com/nil2x/cheburnet/internal/config"
	"github.com/nil2x/cheburnet/internal/datagram"
	"github.com/nil2x/cheburnet/internal/transform"
)

var (
	zeroDatagram      = datagram.New(0, 0, 0, nil)
	zeroDatagramRU    = datagram.Encode(zeroDatagram, transform.Base85CharsetRU)
	zeroDatagramASCII = datagram.Encode(zeroDatagram, transform.Base85CharsetASCII)
)

type executorI interface {
	execute(sendingPlan) error
	wait()
	havePosts() bool
	haveTopics() bool
}

// executor is a component of Session. It responsible for executing a plan that was
// created by the planner. The plan may consist of multiple datagrams, they all will
// be executed and tracked.
//
// Note that datagrams are sent without any particular order. On the receiver side
// they also will be delivered without any particular order. You must assume that
// delivering of any datagram may fail.
type executor struct {
	id       datagram.Ses
	cfg      config.Config
	wg       sync.WaitGroup
	mu       sync.Mutex
	vkC      *api.VKClient
	storageC *api.StorageClient
	posts    map[config.Club]api.WallPostResponse
	topics   map[config.Club]api.BoardAddTopicResponse
}

func newExecutor(cfg config.Config, vkC *api.VKClient, storageC *api.StorageClient, id datagram.Ses) executorI {
	return &executor{
		id:       id,
		cfg:      cfg,
		wg:       sync.WaitGroup{},
		mu:       sync.Mutex{},
		vkC:      vkC,
		storageC: storageC,
		posts:    make(map[config.Club]api.WallPostResponse),
		topics:   make(map[config.Club]api.BoardAddTopicResponse),
	}
}

func (e *executor) execute(plan sendingPlan) error {
	if plan.isEmpty() {
		return nil
	}

	if err := plan.isInvalid(); err != nil {
		return err
	}

	docs := []sendingPlan{}
	qrs := []sendingPlan{}

	for i, method := range plan.methods {
		encoded := plan.encoded[i]
		str := plan.strings[i]
		club := plan.clubs[i]
		user := plan.users[i]

		if method == methodDoc {
			p := sendingPlan{
				encoded:        []string{encoded},
				strings:        []string{str},
				clubs:          []config.Club{club},
				users:          []config.User{user},
				docLinkMethods: []sendingMethod{plan.docLinkMethods[len(docs)]},
			}
			docs = append(docs, p)
			continue
		}

		if method == methodQR {
			p := sendingPlan{
				encoded: []string{encoded},
				strings: []string{str},
				clubs:   []config.Club{club},
				users:   []config.User{user},
			}
			qrs = append(qrs, p)
			continue
		}

		f := func() error {
			mf, err := e.methodToFunc(method)

			if err != nil {
				return err
			}

			return mf(encoded, club, user)
		}

		e.send(f, "method", method, "dg", str)
	}

	if len(docs) > 0 {
		for _, p := range docs {
			encoded := p.encoded[0]
			str := p.strings[0]
			club := p.clubs[0]
			user := p.users[0]
			method := p.docLinkMethods[0]
			f := func() error {
				mf, err := e.methodToFunc(method)

				if err != nil {
					return err
				}

				return e.executeMethodDoc(encoded, club, user, mf)
			}

			e.send(f, "method", methodDoc, "dg", str)
		}
	}

	if len(qrs) > 0 {
		encoded := make([]string, 0, len(qrs))
		slogArgs := make([]any, 0, len(qrs)*2+2)

		slogArgs = append(slogArgs, "method", methodQR)

		for _, p := range qrs {
			encoded = append(encoded, p.encoded[0])
			slogArgs = append(slogArgs, "dg", p.strings[0])
		}

		club := qrs[0].clubs[0]
		user := qrs[0].users[0]
		f := func() error {
			return e.executeMethodQR(encoded, club, user, "")
		}

		e.send(f, slogArgs...)
	}

	return nil
}

func (e *executor) wait() {
	e.wg.Wait()
}

func (e *executor) send(f func() error, slogArgs ...any) {
	args := make([]any, 0, len(slogArgs)+4)
	args = append(args, "id", e.id)
	args = append(args, slogArgs...)

	slog.Debug("session: send", args...)

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()

		if err := f(); err != nil {
			args = append(args, "err", err)
			slog.Error("session: send", args...)
		}
	}()
}

type executorStringFunc func(string, config.Club, config.User) error

func (e *executor) methodToFunc(method sendingMethod) (executorStringFunc, error) {
	var f executorStringFunc

	switch method {
	case methodMessage:
		f = e.executeMethodMessage
	case methodPost:
		f = e.executeMethodPost
	case methodPostComment:
		f = e.executeMethodPostComment
	case methodCaption:
		f = e.executeMethodCaption
	case methodStorage:
		f = e.executeMethodStorage
	case methodDescription:
		f = e.executeMethodDescription
	case methodWebsite:
		f = e.executeMethodWebsite
	case methodVideoComment:
		f = e.executeMethodVideoComment
	case methodPhotoComment:
		f = e.executeMethodPhotoComment
	case methodMarketComment:
		f = e.executeMethodMarketComment
	case methodTopic:
		f = e.executeMethodTopic
	case methodTopicComment:
		f = e.executeMethodTopicComment
	default:
		return nil, fmt.Errorf("unsupported method: %v", method)
	}

	return f, nil
}

func (e *executor) havePosts() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	return len(e.posts) > 0
}

func (e *executor) haveTopics() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	return len(e.topics) > 0
}

func (e *executor) executeMethodMessage(text string, club config.Club, user config.User) error {
	p := api.MessagesSendParams{
		Message: text,
	}
	_, err := e.vkC.MessagesSend(club, user, p)

	return err
}

func (e *executor) executeMethodPost(text string, club config.Club, _ config.User) error {
	p := api.WallPostParams{
		Message: text,
	}
	resp, err := e.vkC.WallPost(club, p)

	if err != nil {
		return err
	}

	e.mu.Lock()

	if len(e.posts) > 5 {
		clear(e.posts)
	}

	e.posts[club] = resp

	e.mu.Unlock()

	return nil
}

func (e *executor) executeMethodPostComment(text string, _ config.Club, _ config.User) error {
	e.mu.Lock()

	clubs := []config.Club{}

	for c := range e.posts {
		clubs = append(clubs, c)
	}

	club := randElem(clubs)
	post := e.posts[club]

	e.mu.Unlock()

	p := api.WallCreateCommentParams{
		PostID:  post.PostID,
		Message: text,
	}
	_, err := e.vkC.WallCreateComment(club, p)

	return err
}

func (e *executor) executeMethodDoc(text string, club config.Club, user config.User, linkF executorStringFunc) error {
	uploadP := api.DocsUploadParams{
		Data: []byte(text),
	}
	resp, err := e.vkC.DocsUploadAndSave(club, uploadP)

	if err != nil {
		return err
	}

	uri := transform.AddQuery(resp.Doc.URL, transform.Query{Caption: zeroDatagramASCII})
	msg := transform.ToTextURL(uri)

	return linkF(msg, club, user)
}

func (e *executor) executeMethodQR(text []string, club config.Club, user config.User, caption string) error {
	qrs := make([][]byte, len(text))

	for i, content := range text {
		qr, err := transform.EncodeQR(content, e.cfg.QR.ImageSize, transform.QRLevel(e.cfg.QR.ImageLevel))

		if err != nil {
			return fmt.Errorf("encode: %v", err)
		}

		qrs[i] = qr
	}

	qr, err := transform.MergeQR(qrs, e.cfg.QR.ImageSize)

	if err != nil {
		return fmt.Errorf("merge: %v", err)
	}

	if len(caption) == 0 {
		// zero caption indicates that QR have meaningful content inside.
		// non-zero caption indicates that QR have no meaningful content inside.
		caption = zeroDatagramRU
	}

	p := api.PhotosUploadAndSaveParams{
		PhotosUploadParams: api.PhotosUploadParams{
			Data: qr,
		},
		PhotosSaveParams: api.PhotosSaveParams{
			Caption: caption,
		},
	}

	if _, err := e.vkC.PhotosUploadAndSave(club, user, p); err != nil {
		return fmt.Errorf("upload: %v", err)
	}

	return nil
}

func (e *executor) executeMethodCaption(text string, club config.Club, user config.User) error {
	return e.executeMethodQR([]string{zeroDatagramASCII}, club, user, text)
}

func (e *executor) executeMethodStorage(text string, club config.Club, _ config.User) error {
	p := api.StorageSetParams{
		Key:   e.storageC.CreateSetKey(),
		Value: text,
	}
	err := e.vkC.StorageSet(club, p)

	return err
}

func (e *executor) executeMethodDescription(text string, club config.Club, _ config.User) error {
	p := api.GroupsEditParams{
		Description: text,
	}
	err := e.vkC.GroupsEdit(club, p)

	return err
}

func (e *executor) executeMethodWebsite(text string, club config.Club, _ config.User) error {
	p := api.GroupsEditParams{
		Website: text,
	}
	err := e.vkC.GroupsEdit(club, p)

	return err
}

func (e *executor) executeMethodVideoComment(text string, club config.Club, user config.User) error {
	p := api.VideoCreateCommentParams{
		Message: text,
	}
	_, err := e.vkC.VideoCreateComment(club, user, p)

	return err
}

func (e *executor) executeMethodPhotoComment(text string, club config.Club, user config.User) error {
	p := api.PhotosCreateCommentParams{
		Message: text,
	}
	_, err := e.vkC.PhotosCreateComment(club, user, p)

	return err
}

func (e *executor) executeMethodMarketComment(text string, club config.Club, user config.User) error {
	p := api.MarketCreateCommentParams{
		Message: text,
	}
	_, err := e.vkC.MarketCreateComment(club, user, p)

	return err
}

func (e *executor) executeMethodTopic(text string, club config.Club, user config.User) error {
	p := api.BoardAddTopicParams{
		Title: zeroDatagramRU,
		Text:  text,
	}
	resp, err := e.vkC.BoardAddTopic(club, user, p)

	if err != nil {
		return err
	}

	e.mu.Lock()

	if len(e.topics) > 5 {
		clear(e.topics)
	}

	e.topics[club] = resp

	e.mu.Unlock()

	return nil
}

func (e *executor) executeMethodTopicComment(text string, _ config.Club, user config.User) error {
	e.mu.Lock()

	clubs := []config.Club{}

	for c := range e.topics {
		clubs = append(clubs, c)
	}

	club := randElem(clubs)
	topic := e.topics[club]

	e.mu.Unlock()

	p := api.BoardCreateCommentParams{
		TopicID: topic.ID,
		Message: text,
	}
	_, err := e.vkC.BoardCreateComment(club, user, p)

	return err
}
