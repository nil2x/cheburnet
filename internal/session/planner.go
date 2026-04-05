package session

import (
	"errors"
	"math/rand"
	"sync"

	"github.com/nil2x/cheburnet/internal/config"
	"github.com/nil2x/cheburnet/internal/datagram"
	"github.com/nil2x/cheburnet/internal/imap"
	"github.com/nil2x/cheburnet/internal/transform"
	"github.com/nil2x/cheburnet/internal/yadisk"
)

type sendingMethod int

const (
	methodMessage sendingMethod = iota + 1
	methodPost
	methodPostComment
	methodDoc
	methodQR
	methodCaption
	methodStorage
	methodDescription
	methodWebsite
	methodVideoComment
	methodPhotoComment
	methodMarketComment
	methodTopic
	methodTopicComment
	methodIMAP
	methodYaDisk
)

var (
	methodsEnabled       = map[sendingMethod]bool{}
	methodsEncoding      = map[sendingMethod]transform.Base85Charset{}
	methodsMaxLenEncoded = map[sendingMethod]int{}
	methodsMaxLenPayload = map[sendingMethod]int{}
)

func initPlanner(cfg config.Config) {
	methodsEnabled = map[sendingMethod]bool{
		methodMessage:       true,
		methodPost:          true,
		methodPostComment:   true,
		methodDoc:           true,
		methodQR:            !(cfg.API.Unathorized || len(cfg.QR.ZBarPath) == 0),
		methodCaption:       !cfg.API.Unathorized,
		methodStorage:       true,
		methodDescription:   false, // disabled, too early flood control
		methodWebsite:       false, // disabled, too early flood control
		methodVideoComment:  !cfg.API.Unathorized,
		methodPhotoComment:  !cfg.API.Unathorized,
		methodMarketComment: !cfg.API.Unathorized,
		methodTopic:         false && !cfg.API.Unathorized, // disabled, captcha control
		methodTopicComment:  false && !cfg.API.Unathorized, // disabled, captcha control
		methodIMAP:          imap.HaveClients(),
		methodYaDisk:        yadisk.HaveClients(),
	}
	methodsMaxLenEncoded = map[sendingMethod]int{
		methodMessage:       4096,
		methodPost:          16000,
		methodPostComment:   16000,
		methodDoc:           1 * 1024 * 1024,
		methodQR:            transform.QRMaxLen[transform.QRLevel(cfg.QR.ImageLevel)],
		methodCaption:       2048,
		methodStorage:       4096,
		methodDescription:   3000,
		methodWebsite:       200,
		methodVideoComment:  4096,
		methodPhotoComment:  2048,
		methodMarketComment: 2048,
		methodTopic:         4096,
		methodTopicComment:  4096,
		methodIMAP:          512 * 1024,
		methodYaDisk:        1 * 1024 * 1024,
	}

	for method, enabled := range cfg.Session.MethodsEnabled {
		methodsEnabled[sendingMethod(method)] = enabled
	}

	for method, len := range cfg.Session.MethodsMaxLenEncoded {
		methodsMaxLenEncoded[sendingMethod(method)] = len
	}

	methodsEncoding = map[sendingMethod]transform.Base85Charset{
		methodMessage:       transform.Base85CharsetRU,
		methodPost:          transform.Base85CharsetRU,
		methodPostComment:   transform.Base85CharsetRU,
		methodDoc:           transform.Base85CharsetASCII,
		methodQR:            transform.Base85CharsetASCII,
		methodCaption:       transform.Base85CharsetRU,
		methodStorage:       transform.Base85CharsetASCII,
		methodDescription:   transform.Base85CharsetASCII,
		methodWebsite:       transform.Base85CharsetASCII,
		methodVideoComment:  transform.Base85CharsetRU,
		methodPhotoComment:  transform.Base85CharsetRU,
		methodMarketComment: transform.Base85CharsetRU,
		methodTopic:         transform.Base85CharsetRU,
		methodTopicComment:  transform.Base85CharsetRU,
		methodIMAP:          transform.Base85CharsetASCII,
		methodYaDisk:        transform.Base85CharsetASCII,
	}
	methodsMaxLenPayload = map[sendingMethod]int{
		methodMessage:       datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodMessage]),
		methodPost:          datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodPost]),
		methodPostComment:   datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodPostComment]),
		methodDoc:           datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodDoc]),
		methodQR:            datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodQR]),
		methodCaption:       datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodCaption]),
		methodStorage:       datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodStorage]),
		methodDescription:   datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodDescription]),
		methodWebsite:       datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodWebsite]),
		methodVideoComment:  datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodVideoComment]),
		methodPhotoComment:  datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodPhotoComment]),
		methodMarketComment: datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodMarketComment]),
		methodTopic:         datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodTopic]),
		methodTopicComment:  datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodTopicComment]),
		methodIMAP:          datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodIMAP]),
		methodYaDisk:        datagram.CalcMaxLenPayload(methodsMaxLenEncoded[methodYaDisk]),
	}
}

type sendingPlan struct {
	methods        []sendingMethod
	fragments      []datagram.Datagram
	encoded        []string
	strings        []string
	clubs          []config.Club
	users          []config.User
	imap           []*imap.Client
	yadisk         []*yadisk.Client
	docLinkMethods []sendingMethod
}

func (p sendingPlan) isEmpty() bool {
	return len(p.methods) == 0
}

func (p sendingPlan) isInvalid() error {
	if len(p.methods) != len(p.fragments) {
		return errors.New("methods and fragments mismatch")
	}

	if len(p.methods) != len(p.encoded) {
		return errors.New("methods and encoded mismatch")
	}

	if len(p.methods) != len(p.strings) {
		return errors.New("methods and strings mismatch")
	}

	if len(p.methods) != len(p.clubs) {
		return errors.New("methods and clubs mismatch")
	}

	if len(p.methods) != len(p.users) {
		return errors.New("methods and users mismatch")
	}

	if len(p.methods) != len(p.imap) {
		return errors.New("methods and imap mismatch")
	}

	if len(p.methods) != len(p.yadisk) {
		return errors.New("methods and yadisk mismatch")
	}

	methodDocCount := 0

	for _, method := range p.methods {
		if method == methodDoc {
			methodDocCount++
		}
	}

	if methodDocCount != len(p.docLinkMethods) {
		return errors.New("methodDoc misconfiguration")
	}

	return nil
}

// planner is a component of Session. It responsible for creating most efficient
// sending plan for a given datagram. The datagram, if possible, may be split into
// smaller datagrams, thus the plan may consist of either one or multiple items.
//
// The plan is needed because sending happen using third party API, which have limitations.
// To ensure that datagrams successfully reach a remote peer over third party API we have to
// use this API efficiently. That's what planner do. Though, you must not assume that all
// datagrams will be delivered successfully as any plan may fail for independent reason.
type planner struct {
	cfg      config.Config
	session  *Session
	executor executorI
	mu       sync.Mutex
	number   int
}

func newPlanner(cfg config.Config, s *Session, e executorI) *planner {
	return &planner{
		cfg:      cfg,
		session:  s,
		executor: e,
		mu:       sync.Mutex{},
		number:   0,
	}
}

func (p *planner) create(dg datagram.Datagram) (sendingPlan, error) {
	p.mu.Lock()

	p.number++
	num := p.number

	p.mu.Unlock()

	plan, err := p.createPlan(dg, num)

	if err != nil {
		return sendingPlan{}, err
	}

	plan.clubs = p.createClubs(plan.methods)
	plan.users = p.createUsers(plan.methods)
	plan.imap = p.createIMAP(plan.methods)
	plan.yadisk = p.createYaDisk(plan.methods)

	docLinkMethods, err := p.createDocLinkMethods(plan.methods)

	if err != nil {
		return sendingPlan{}, err
	}

	plan.docLinkMethods = docLinkMethods

	return plan, nil
}

func (p *planner) createPlan(dg datagram.Datagram, num int) (sendingPlan, error) {
	smallMethods := []sendingMethod{
		methodMessage,
		methodPost,
		methodVideoComment,
		methodPhotoComment,
		methodMarketComment,
		methodTopic,
	}
	bigMethods := []sendingMethod{
		methodDoc,
		methodIMAP, methodIMAP,
		methodYaDisk,
	}

	// Try to detect if TLS handshake is in progress.
	isHandshakeTLS := (num <= 3) && (len(dg.Payload) <= 10000)

	if !isHandshakeTLS {
		if methodsEnabled[methodQR] {
			smallMethods = append(smallMethods, methodQR)
		} else if methodsEnabled[methodCaption] {
			smallMethods = append(smallMethods, methodCaption)
		}

		smallMethods = append(smallMethods, methodIMAP)
	}

	if p.executor.havePosts() {
		smallMethods = append(smallMethods, methodPostComment, methodPostComment)
	}

	if p.executor.haveTopics() {
		smallMethods = append(smallMethods, methodTopicComment)
	}

	// methodStorage can't be used for CommandConnect because storage listener
	// is not able to detect first command of the session.
	if dg.Command != datagram.CommandConnect {
		smallMethods = append(smallMethods, methodStorage, methodStorage)
	}

	smallMethods = filterOutDisabledMethods(smallMethods)
	bigMethods = filterOutDisabledMethods(bigMethods)

	if len(smallMethods) == 0 {
		return sendingPlan{}, errors.New("no small methods available")
	}

	if len(bigMethods) == 0 {
		return sendingPlan{}, errors.New("no big methods available")
	}

	plan := sendingPlan{
		methods:   []sendingMethod{},
		fragments: []datagram.Datagram{},
		encoded:   []string{},
		strings:   []string{},
	}

	// Small datagrams goes this way.
	if dg.Command != datagram.CommandForward {
		if dg.Number == 0 {
			dg.Number = p.session.nextNumber()
		}

		method := randElem(smallMethods)
		plan.methods = append(plan.methods, method)
		plan.fragments = append(plan.fragments, dg)
		plan.encoded = append(plan.encoded, datagram.Encode(dg, methodsEncoding[method]))
		plan.strings = append(plan.strings, dg.String())

		return plan, nil
	}

	const smallForwardLen = 4096

	// Numbered datagrams (typically, from history) goes this way.
	if dg.Number != 0 {
		available := []sendingMethod{}

		for _, m := range smallMethods {
			if dg.LenEncoded() <= methodsMaxLenEncoded[m] {
				available = append(available, m)
			}
		}

		if dg.LenEncoded() > smallForwardLen || len(available) == 0 {
			for _, m := range bigMethods {
				if dg.LenEncoded() <= methodsMaxLenEncoded[m] {
					available = append(available, m)
				}
			}
		}

		if len(available) == 0 {
			return sendingPlan{}, errors.New("no numbered methods available")
		}

		method := randElem(available)
		plan.methods = append(plan.methods, method)
		plan.fragments = append(plan.fragments, dg)
		plan.encoded = append(plan.encoded, datagram.Encode(dg, methodsEncoding[method]))
		plan.strings = append(plan.strings, dg.String())

		return plan, nil
	}

	// Forwards that can be split goes this way.
	for len(dg.Payload) > 0 {
		var method sendingMethod

		// During TLS handshake a datagram should be sent as fast as possible.
		// It is needed because some sites enforce too short TLS handshake timeout.
		// In case of datagram delay on remote side a site may close the connection.
		if isHandshakeTLS {
			method = randElem(smallMethods)
		} else if dg.LenEncoded() <= smallForwardLen {
			method = randElem(smallMethods)
		} else {
			method = randElem(bigMethods)
		}

		chunks := transform.BytesToChunks(dg.Payload, methodsMaxLenPayload[method], 2)

		if len(chunks) == 0 || len(chunks) > 2 {
			return sendingPlan{}, errors.New("invalid chunks logic")
		}

		if len(chunks) == 2 {
			dg.Payload = chunks[1]
		} else {
			dg.Payload = nil
		}

		fg := datagram.New(dg.Session, p.session.nextNumber(), dg.Command, chunks[0])

		if fg.LenEncoded() > methodsMaxLenEncoded[method] {
			return sendingPlan{}, errors.New("invalid payload logic")
		}

		plan.methods = append(plan.methods, method)
		plan.fragments = append(plan.fragments, fg)
		plan.encoded = append(plan.encoded, datagram.Encode(fg, methodsEncoding[method]))
		plan.strings = append(plan.strings, fg.String())

		if len(plan.methods) > 1000 {
			return sendingPlan{}, errors.New("infinite loop protection")
		}
	}

	return plan, nil
}

func (p *planner) createClubs(methods []sendingMethod) []config.Club {
	clubs := make([]config.Club, len(methods))

	for i := range methods {
		clubs[i] = randElem(p.cfg.Clubs)
	}

	return clubs
}

func (p *planner) createUsers(methods []sendingMethod) []config.User {
	users := make([]config.User, len(methods))

	for i := range methods {
		users[i] = randElem(p.cfg.Users)
	}

	return users
}

func (p *planner) createIMAP(methods []sendingMethod) []*imap.Client {
	clients := make([]*imap.Client, len(methods))

	for i := range methods {
		cfg := randElem(p.cfg.IMAP)
		c, exists := imap.GetClient(cfg.Name)

		if exists {
			clients[i] = c
		}
	}

	return clients
}

func (p *planner) createYaDisk(methods []sendingMethod) []*yadisk.Client {
	clients := make([]*yadisk.Client, len(methods))

	for i := range methods {
		cfg := randElem(p.cfg.YaDisk)
		c, exists := yadisk.GetClient(cfg.Name)

		if exists {
			clients[i] = c
		}
	}

	return clients
}

func (p *planner) createDocLinkMethods(methods []sendingMethod) ([]sendingMethod, error) {
	available := []sendingMethod{
		methodMessage,
		methodPost,
		methodStorage, methodStorage,
		methodDescription,
		methodWebsite,
		methodCaption,
		methodVideoComment,
		methodPhotoComment,
		methodMarketComment,
		methodTopic,
		methodIMAP,
	}

	if p.executor.havePosts() {
		available = append(available, methodPostComment, methodPostComment)
	}

	if p.executor.haveTopics() {
		available = append(available, methodTopicComment)
	}

	available = filterOutDisabledMethods(available)

	if len(available) == 0 {
		return nil, errors.New("no doc link methods available")
	}

	linkMethods := []sendingMethod{}

	for _, method := range methods {
		if method == methodDoc {
			linkMethods = append(linkMethods, randElem(available))
		}
	}

	return linkMethods, nil
}

func filterOutDisabledMethods(methods []sendingMethod) []sendingMethod {
	enabled := []sendingMethod{}

	for _, method := range methods {
		if methodsEnabled[method] {
			enabled = append(enabled, method)
		}
	}

	return enabled
}

func randElem[T any](elems []T) T {
	if len(elems) == 0 {
		return *new(T)
	}

	n := rand.Intn(len(elems))

	return elems[n]
}
