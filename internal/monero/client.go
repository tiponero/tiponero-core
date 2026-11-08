package monero

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/icholy/digest"
)

var ErrNotConfigured = errors.New("wallet RPC not configured")

const rpcTimeout = 30 * time.Second

type Client struct {
	url  string
	http *http.Client
}

func NewClient(url, username, password string) *Client {
	c := &Client{url: NormalizeRPCURL(url), http: &http.Client{Timeout: rpcTimeout}}
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

// NormalizeRPCURL accepts both "http://host:port" and
// "http://host:port/json_rpc". The client always appends /json_rpc, so the
// suffix is removed when the operator pastes a full endpoint.
func NormalizeRPCURL(raw string) string {
	u := strings.TrimSpace(raw)
	u = strings.TrimRight(u, "/")
	u = strings.TrimSuffix(u, "/json_rpc")
	return strings.TrimRight(u, "/")
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
	defer func() { _ = resp.Body.Close() }()

	var envelope rpcResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if envelope.Error != nil {
		return envelope.Error
	}

	return json.Unmarshal(envelope.Result, result)
}

// GenerateFromKeys creates a wallet file inside the wallet-rpc wallet
// directory. With no spend key the result is a view-only wallet, which is the
// only kind Tiponero creates.
func (c *Client) GenerateFromKeys(p GenerateFromKeysParams) error {
	var result struct {
		Address string `json:"address"`
	}
	return c.call("generate_from_keys", p, &result)
}
