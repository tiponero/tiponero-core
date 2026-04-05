package monero

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse[T any] struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      string    `json:"id"`
	Result  T         `json:"result"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

type CreateAddressParams struct {
	AccountIndex uint64 `json:"account_index"`
	Label        string `json:"label,omitempty"`
}

type CreateAddressResult struct {
	Address      string `json:"address"`
	AddressIndex uint64 `json:"address_index"`
}

type GetTransfersParams struct {
	In      bool `json:"in"`
	Pending bool `json:"pending"`
	Pool    bool `json:"pool"`
}

type Transfer struct {
	Address       string       `json:"address"`
	Amount        uint64       `json:"amount"`
	Confirmations uint64       `json:"confirmations"`
	TxHash        string       `json:"txid"`
	SubaddrIndex  SubaddrIndex `json:"subaddr_index"`
	Type          string       `json:"type"`
}

type SubaddrIndex struct {
	Major uint64 `json:"major"`
	Minor uint64 `json:"minor"`
}

type GetTransfersResult struct {
	In      []Transfer `json:"in"`
	Pending []Transfer `json:"pending"`
	Pool    []Transfer `json:"pool"`
}

type GetHeightResult struct {
	Height uint64 `json:"height"`
}

type OpenWalletParams struct {
	Filename string `json:"filename"`
	Password string `json:"password,omitempty"`
}

type ConnectionStatus struct {
	Configured  bool
	Connected   bool
	Host        string
	Port        string
	BlockHeight uint64
	Error       string
}
