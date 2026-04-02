package imap

import (
	"sync"
)

var clients = map[string]*Client{}
var clientsMu = sync.Mutex{}

// SetClient sets client in the global state.
func SetClient(name string, c *Client) {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	clients[name] = c
}

// GetClient returns client from the global state.
func GetClient(name string) (*Client, bool) {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	c, exists := clients[name]

	return c, exists
}

// GetClients returns all clients from the global state.
func GetClients() []*Client {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	result := make([]*Client, 0, len(clients))

	for _, c := range clients {
		result = append(result, c)
	}

	return result
}

// HaveClients returns if the global state have at least one client.
func HaveClients() bool {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	return len(clients) > 0
}

func clearClients() {
	clientsMu.Lock()
	defer clientsMu.Unlock()

	clear(clients)
}
