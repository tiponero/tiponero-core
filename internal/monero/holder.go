package monero

import (
	"net/url"
	"sync"
)

type ClientHolder struct {
	mu     sync.RWMutex
	client *Client
}

func NewClientHolder(c *Client) *ClientHolder {
	return &ClientHolder{client: c}
}

func (h *ClientHolder) Get() *Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.client
}

func (h *ClientHolder) Set(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.client = c
}

func (h *ClientHolder) Status() ConnectionStatus {
	c := h.Get()
	status := ConnectionStatus{}

	rawURL := c.URL()
	if rawURL == "" {
		return status
	}

	status.Configured = true
	if u, err := url.Parse(rawURL); err == nil {
		status.Host = u.Hostname()
		status.Port = u.Port()
	}

	height, err := c.GetHeight()
	if err != nil {
		status.Error = err.Error()
		return status
	}

	status.Connected = true
	status.BlockHeight = height
	return status
}
