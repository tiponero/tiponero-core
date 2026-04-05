package monero

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/icholy/digest"
)

var ErrNotConfigured = errors.New("wallet RPC not configured")

type Client struct {
	url  string
	http *http.Client
}

func NewClient(url, username, password string) *Client {
	c := &Client{url: strings.TrimRight(url, "/"), http: &http.Client{}}
	if username != "" {
		c.http.Transport = &digest.Transport{
			Username: username,
			Password: password,
		}
	}
	return c
}

func (c *Client) CreateAddress(label string) (string, uint64, error) {
	var result CreateAddressResult
	err := c.call("create_address", CreateAddressParams{
		AccountIndex: 0,
		Label:        label,
	}, &result)
	return result.Address, result.AddressIndex, err
}

func (c *Client) GetTransfers() (*GetTransfersResult, error) {
	var result GetTransfersResult
	err := c.call("get_transfers", GetTransfersParams{
		In:      true,
		Pending: true,
		Pool:    true,
	}, &result)
	return &result, err
}

func (c *Client) GetHeight() (uint64, error) {
	var result GetHeightResult
	err := c.call("get_height", nil, &result)
	return result.Height, err
}

func (c *Client) OpenWallet(filename, password string) error {
	var result struct{}
	return c.call("open_wallet", OpenWalletParams{
		Filename: filename,
		Password: password,
	}, &result)
}

func (c *Client) URL() string {
	return c.url
}

func (c *Client) call(method string, params any, result any) error {
	if c.url == "" {
		return ErrNotConfigured
	}

	body, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      "0",
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", c.url+"/json_rpc", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("rpc call %s: %w", method, err)
	}
	defer resp.Body.Close()

	var envelope rpcResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if envelope.Error != nil {
		return envelope.Error
	}

	return json.Unmarshal(envelope.Result, result)
}
