package session

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nil2x/cheburnet/internal/api"
	"github.com/nil2x/cheburnet/internal/config"
	"github.com/nil2x/cheburnet/internal/datagram"
)

var muxer executorI

func initMultiplexer(cfg config.Config, vkC *api.VKClient, storageC *api.StorageClient) {
	muxer = newMultiplexer(cfg, vkC, storageC)
}

// multiplexer is a type of executor. It collects different plans that were created
// by the planner, merges them together into a single plan preserving limits that
// are enforced by the planner, and executes resulted plan using underlying executor.
// This allows to merge multiple separate datagrams together into a single encoded object,
// leading to more efficient API usage at the cost of increased sending delay.
//
// To use multiplexer, call initMultiplexer at the program start and use global muxer object.
// You should use only one multiplexer instance per program. muxer may be nil which means
// initMultiplexer wasn't called and you shouldn't use it, in this case use executor directly.
//
// The multiplexer does not create new sending plan, instead it merges plans that can be merged.
// Plans that can't be merged are kept as is. For example, if the planner decided that datagrams
// 1 and 2 should be sent using method 5, and capacity of method 5 can hold both datagrams, then
// there will be one call of method 5 with both datagrams instead of two calls. But if the planner
// decided that datagram 1 should be sent using method 5 and datagram 2 should be sent using method 7,
// then these plans can't be merged and methods 5 and 7 will be executed without any modifiction.
type multiplexer struct {
	mu       sync.Mutex
	buffer   []sendingPlan
	executor executorI
}

func newMultiplexer(cfg config.Config, vkC *api.VKClient, storageC *api.StorageClient) executorI {
	interval := cfg.Session.MuxInterval()

	if interval == 0 {
		return nil
	}

	m := &multiplexer{
		mu:       sync.Mutex{},
		buffer:   []sendingPlan{},
		executor: newExecutor(cfg, vkC, storageC, 0),
	}

	go func() {
		for {
			<-time.After(interval)

			if err := m.flush(); err != nil {
				slog.Error("session: mux", "err", err)
			}
		}
	}()

	return m
}

func (m *multiplexer) execute(plan sendingPlan) error {
	if err := plan.isInvalid(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.buffer = append(m.buffer, plan)

	return nil
}

func (m *multiplexer) wait() {
	m.executor.wait()
}

func (m *multiplexer) havePosts() bool {
	return m.executor.havePosts()
}

func (m *multiplexer) haveTopics() bool {
	return m.executor.haveTopics()
}

func (m *multiplexer) flush() error {
	m.mu.Lock()

	b := m.buffer
	m.buffer = []sendingPlan{}

	m.mu.Unlock()

	plan, err := m.merge(b)

	if err != nil {
		return fmt.Errorf("merge: %v", err)
	}

	if err := m.executor.execute(plan); err != nil {
		return fmt.Errorf("execute: %v", err)
	}

	return nil
}

func (m *multiplexer) merge(plans []sendingPlan) (sendingPlan, error) {
	if len(plans) == 0 {
		return sendingPlan{}, nil
	}

	type metadata struct {
		index, lenEncoded int
	}

	var (
		errInvalidByMethod   = errors.New("invalid byMethod logic")
		errInvalidByLenMuxed = errors.New("invalid byLenMuxed logic")
	)

	byMethod := map[sendingMethod]sendingPlan{}
	byMethodMeta := map[sendingMethod][]metadata{}

	// group plans by sending method and record metadata about each datagram
	for _, plan := range plans {
		for i, method := range plan.methods {
			grouped, exists := byMethod[method]

			if !exists {
				grouped = sendingPlan{
					fragments:        []datagram.Datagram{},
					encoded:          []string{},
					strings:          []string{},
					clubs:            []config.Club{},
					users:            []config.User{},
					methodDocMethods: []sendingMethod{},
				}
				byMethodMeta[method] = []metadata{}
			}

			grouped.fragments = append(grouped.fragments, plan.fragments[i])
			grouped.encoded = append(grouped.encoded, plan.encoded[i])
			grouped.strings = append(grouped.strings, plan.strings[i])
			grouped.clubs = append(grouped.clubs, plan.clubs[i])
			grouped.users = append(grouped.users, plan.users[i])
			byMethod[method] = grouped

			meta := metadata{
				index:      len(grouped.fragments) - 1,
				lenEncoded: plan.fragments[i].LenEncoded(),
			}
			byMethodMeta[method] = append(byMethodMeta[method], meta)
		}

		if len(plan.methodDocMethods) > 0 {
			grouped, exists := byMethod[methodDoc]

			if !exists {
				return sendingPlan{}, errInvalidByMethod
			}

			grouped.methodDocMethods = append(grouped.methodDocMethods, plan.methodDocMethods...)
			byMethod[methodDoc] = grouped
		}
	}

	byLenMuxed := map[sendingMethod][][]metadata{}

	// group plans, so that each group fits into maximum encoding length limit
	for method, items := range byMethodMeta {
		byLenMuxed[method] = [][]metadata{}

		// small datagrams should be grouped first as it is more efficient
		sort.Slice(items, func(i, j int) bool {
			return items[i].lenEncoded < items[j].lenEncoded
		})

		currGroup := []metadata{}
		currLenEncoded := 0
		processedItems := 0

		for i, item := range items {
			nextLenGroup := len(currGroup) + 1
			nextLenEncoded := currLenEncoded + item.lenEncoded
			nextLenMuxed := nextLenEncoded + datagram.MuxLen(nextLenGroup)

			if nextLenMuxed > methodsMaxLenEncoded[method] {
				if len(currGroup) == 0 {
					return sendingPlan{}, errInvalidByLenMuxed
				}

				byLenMuxed[method] = append(byLenMuxed[method], currGroup)
				processedItems += len(currGroup)

				currGroup = []metadata{}
				currLenEncoded = 0
			}

			currGroup = append(currGroup, item)
			currLenEncoded += item.lenEncoded

			if i == len(items)-1 {
				currLenGroup := len(currGroup)
				currLenMuxed := currLenEncoded + datagram.MuxLen(currLenGroup)

				if currLenMuxed > methodsMaxLenEncoded[method] {
					return sendingPlan{}, errInvalidByLenMuxed
				}

				byLenMuxed[method] = append(byLenMuxed[method], currGroup)
				processedItems += len(currGroup)

				currGroup = []metadata{}
				currLenEncoded = 0
			}
		}

		if len(currGroup) != 0 || len(items) != processedItems {
			return sendingPlan{}, errInvalidByLenMuxed
		}
	}

	// additional check that groups have no overlap and byLenMuxed is correct
	for method, groups := range byLenMuxed {
		if _, exists := byMethod[method]; !exists {
			return sendingPlan{}, errInvalidByLenMuxed
		}

		indexes := map[int]struct{}{}
		items := 0
		maxIndex := len(byMethod[method].encoded) - 1

		for _, group := range groups {
			for _, item := range group {
				indexes[item.index] = struct{}{}
				items++

				if item.index > maxIndex {
					return sendingPlan{}, errInvalidByLenMuxed
				}
			}
		}

		if items == 0 || len(indexes) != items {
			return sendingPlan{}, errInvalidByLenMuxed
		}
	}

	merged := sendingPlan{
		methods:          []sendingMethod{},
		fragments:        []datagram.Datagram{},
		encoded:          []string{},
		strings:          []string{},
		clubs:            []config.Club{},
		users:            []config.User{},
		methodDocMethods: []sendingMethod{},
	}

	// merge groups whose method is same and mux their datagrams
	for method, groups := range byLenMuxed {
		for _, group := range groups {
			encoded := make([]string, len(group))
			strs := make([]string, len(group))

			for i, item := range group {
				encoded[i] = byMethod[method].encoded[item.index]
				strs[i] = byMethod[method].strings[item.index]
			}

			muxed := datagram.Mux(encoded)
			str := strings.Join(strs, " ")

			merged.methods = append(merged.methods, method)
			merged.fragments = append(merged.fragments, datagram.Datagram{})
			merged.encoded = append(merged.encoded, muxed)
			merged.strings = append(merged.strings, str)
			merged.clubs = append(merged.clubs, randElem(byMethod[method].clubs))
			merged.users = append(merged.users, randElem(byMethod[method].users))

			if method == methodDoc {
				merged.methodDocMethods = append(merged.methodDocMethods, randElem(byMethod[methodDoc].methodDocMethods))
			}
		}
	}

	return merged, nil
}
