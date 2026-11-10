package chains

import "strings"

// Namespace groups chains that share an address format and signing scheme.
// One verified wallet in a namespace can receive on every chain in it.
type Namespace string

const (
	NamespaceEVM      Namespace = "evm"
	NamespaceSolana   Namespace = "solana"
	NamespaceBitcoin  Namespace = "bitcoin"
	NamespaceLitecoin Namespace = "litecoin"
	NamespaceTron     Namespace = "tron"
)

type Chain struct {
	Key        string    `json:"key"`
	Name       string    `json:"name"`
	Namespace  Namespace `json:"namespace"`
	EVMChainID int64     `json:"evmChainId,omitempty"`
	Testnet    bool      `json:"testnet"`
	NativeCoin string    `json:"nativeCoin"`
	// NativeDecimals is the precision of the native coin's base unit (wei, lamports).
	NativeDecimals int `json:"nativeDecimals"`
	// Confirmations is the block depth MAX waits for before marking an EVM
	// payment completed. Solana payments wait for "finalized" commitment instead.
	Confirmations int64  `json:"confirmations"`
	ExplorerTxURL string `json:"explorerTxUrl"`
	// ReceiveOnly chains resolve names to pasted addresses. MAX never sends
	// or tracks payments on them; cross-chain routes may deliver to them.
	ReceiveOnly bool `json:"receiveOnly"`
}

// Supported is the explicit list of chains MAX resolves for. Anything not
// listed here is rejected rather than guessed.
var Supported = []Chain{
	{Key: "ethereum", Name: "Ethereum", Namespace: NamespaceEVM, EVMChainID: 1, NativeCoin: "ETH", NativeDecimals: 18, Confirmations: 3, ExplorerTxURL: "https://etherscan.io/tx/"},
	{Key: "base", Name: "Base", Namespace: NamespaceEVM, EVMChainID: 8453, NativeCoin: "ETH", NativeDecimals: 18, Confirmations: 2, ExplorerTxURL: "https://basescan.org/tx/"},
	{Key: "arbitrum", Name: "Arbitrum One", Namespace: NamespaceEVM, EVMChainID: 42161, NativeCoin: "ETH", NativeDecimals: 18, Confirmations: 2, ExplorerTxURL: "https://arbiscan.io/tx/"},
	{Key: "optimism", Name: "OP Mainnet", Namespace: NamespaceEVM, EVMChainID: 10, NativeCoin: "ETH", NativeDecimals: 18, Confirmations: 2, ExplorerTxURL: "https://optimistic.etherscan.io/tx/"},
	{Key: "polygon", Name: "Polygon PoS", Namespace: NamespaceEVM, EVMChainID: 137, NativeCoin: "POL", NativeDecimals: 18, Confirmations: 20, ExplorerTxURL: "https://polygonscan.com/tx/"},
	{Key: "bnb", Name: "BNB Chain", Namespace: NamespaceEVM, EVMChainID: 56, NativeCoin: "BNB", NativeDecimals: 18, Confirmations: 15, ExplorerTxURL: "https://bscscan.com/tx/"},
	{Key: "avalanche", Name: "Avalanche C-Chain", Namespace: NamespaceEVM, EVMChainID: 43114, NativeCoin: "AVAX", NativeDecimals: 18, Confirmations: 1, ExplorerTxURL: "https://snowtrace.io/tx/"},
	{Key: "bitcoin", Name: "Bitcoin", Namespace: NamespaceBitcoin, NativeCoin: "BTC", NativeDecimals: 8, ExplorerTxURL: "https://mempool.space/tx/", ReceiveOnly: true},
	{Key: "litecoin", Name: "Litecoin", Namespace: NamespaceLitecoin, NativeCoin: "LTC", NativeDecimals: 8, ExplorerTxURL: "https://litecoinspace.org/tx/", ReceiveOnly: true},
	{Key: "tron", Name: "Tron", Namespace: NamespaceTron, NativeCoin: "TRX", NativeDecimals: 6, ExplorerTxURL: "https://tronscan.org/#/transaction/", ReceiveOnly: true},
	{Key: "solana", Name: "Solana", Namespace: NamespaceSolana, NativeCoin: "SOL", NativeDecimals: 9, ExplorerTxURL: "https://solscan.io/tx/"},
	{Key: "sepolia", Name: "Ethereum Sepolia", Namespace: NamespaceEVM, EVMChainID: 11155111, Testnet: true, NativeCoin: "ETH", NativeDecimals: 18, Confirmations: 1, ExplorerTxURL: "https://sepolia.etherscan.io/tx/"},
	{Key: "base-sepolia", Name: "Base Sepolia", Namespace: NamespaceEVM, EVMChainID: 84532, Testnet: true, NativeCoin: "ETH", NativeDecimals: 18, Confirmations: 1, ExplorerTxURL: "https://sepolia.basescan.org/tx/"},
	{Key: "solana-devnet", Name: "Solana Devnet", Namespace: NamespaceSolana, Testnet: true, NativeCoin: "SOL", NativeDecimals: 9, ExplorerTxURL: "https://explorer.solana.com/tx/{tx}?cluster=devnet"},
}

var byKey = func() map[string]Chain {
	m := make(map[string]Chain, len(Supported))
	for _, c := range Supported {
		m[c.Key] = c
	}
	return m
}()

func Lookup(key string) (Chain, bool) {
	c, ok := byKey[strings.ToLower(strings.TrimSpace(key))]
	return c, ok
}

// LinkableNamespaces are the namespaces for which wallet ownership proofs are
// implemented today.
func IsLinkable(ns Namespace) bool {
	return ns == NamespaceEVM || ns == NamespaceSolana
}

// IsPasteable reports whether owners may set a destination in this namespace
// by pasting an address, without a wallet ownership proof.
func IsPasteable(ns Namespace) bool {
	switch ns {
	case NamespaceEVM, NamespaceSolana, NamespaceBitcoin, NamespaceLitecoin, NamespaceTron:
		return true
	}
	return false
}

func ParseNamespace(s string) (Namespace, bool) {
	switch Namespace(strings.ToLower(strings.TrimSpace(s))) {
	case NamespaceEVM:
		return NamespaceEVM, true
	case NamespaceSolana:
		return NamespaceSolana, true
	case NamespaceBitcoin:
		return NamespaceBitcoin, true
	case NamespaceLitecoin:
		return NamespaceLitecoin, true
	case NamespaceTron:
		return NamespaceTron, true
	}
	return "", false
}

// LookupSendable is Lookup for paths that move money: receive-only chains
// are treated as unknown.
func LookupSendable(key string) (Chain, bool) {
	c, ok := Lookup(key)
	if !ok || c.ReceiveOnly {
		return Chain{}, false
	}
	return c, true
}
