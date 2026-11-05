package chains

import (
	"encoding/hex"
	"math/big"
	"strings"
)

// ERC20TransferTopic is keccak256("Transfer(address,address,uint256)").
var ERC20TransferTopic = "0x" + hex.EncodeToString(keccak256([]byte("Transfer(address,address,uint256)")))

const (
	erc20TransferSelector  = "a9059cbb" // transfer(address,uint256)
	ERC20DecimalsCallData  = "0x313ce567"
	erc20BalanceOfSelector = "70a08231" // balanceOf(address)
)

// EncodeERC20Transfer builds calldata for transfer(to, amount).
func EncodeERC20Transfer(lowerTo string, amount *big.Int) string {
	return "0x" + erc20TransferSelector + padAddress(lowerTo) + padUint(amount)
}

// AddressTopic left-pads an address into a 32-byte log topic.
func AddressTopic(lowerAddr string) string {
	return "0x" + padAddress(lowerAddr)
}

func padAddress(lowerAddr string) string {
	return strings.Repeat("0", 24) + strings.TrimPrefix(lowerAddr, "0x")
}

func padUint(v *big.Int) string {
	h := v.Text(16)
	return strings.Repeat("0", 64-len(h)) + h
}

// ParseHexBig parses a 0x-prefixed hex quantity or 32-byte word.
func ParseHexBig(s string) (*big.Int, bool) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if s == "" {
		return new(big.Int), true
	}
	return new(big.Int).SetString(s, 16)
}

// EncodeERC20BalanceOf builds calldata for balanceOf(owner).
func EncodeERC20BalanceOf(lowerOwner string) string {
	return "0x" + erc20BalanceOfSelector + padAddress(lowerOwner)
}
