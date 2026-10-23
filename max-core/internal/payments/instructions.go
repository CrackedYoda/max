package payments

import (
	"github.com/max/max-core/internal/chains"
)

// Instructions tell the sender's wallet exactly what to sign. They are derived
// from the immutable snapshot, never from client input.
type Instructions struct {
	EVM    *EVMInstruction
	Solana *SolanaInstruction
}

type EVMInstruction struct {
	ChainID int64
	From    string
	To      string
	// Value in wei, hex-encoded as wallets expect.
	ValueHex string
	Data     string
}

type SolanaInstructionKind string

const (
	SolanaSystemTransfer     SolanaInstructionKind = "system_transfer"
	SolanaSPLTransferChecked SolanaInstructionKind = "spl_transfer_checked"
)

type SolanaInstruction struct {
	Kind SolanaInstructionKind
	From string
	// To is the recipient wallet (owner); for SPL the tokens land in DestinationTokenAccount.
	To       string
	Lamports string

	Mint                    string
	TokenProgram            string
	Decimals                int
	Amount                  string
	SourceTokenAccount      string
	DestinationTokenAccount string
	// DestinationTokenAccountExists is nil when not checked. Senders should use
	// createAssociatedTokenAccountIdempotent regardless.
	DestinationTokenAccountExists *bool
}

func buildInstructions(p Payment) Instructions {
	switch p.Chain.Namespace {
	case chains.NamespaceEVM:
		ins := &EVMInstruction{ChainID: p.Chain.EVMChainID, From: chains.ChecksumEVMAddress(p.SenderAddress)}
		if p.Asset.Kind == AssetNative {
			ins.To = chains.ChecksumEVMAddress(p.PayTo())
			ins.ValueHex = "0x" + p.Amount.Text(16)
			ins.Data = "0x"
		} else {
			ins.To = chains.ChecksumEVMAddress(p.Asset.Address)
			ins.ValueHex = "0x0"
			ins.Data = chains.EncodeERC20Transfer(p.PayTo(), p.Amount)
		}
		return Instructions{EVM: ins}

	case chains.NamespaceSolana:
		if p.Asset.Kind == AssetNative {
			return Instructions{Solana: &SolanaInstruction{
				Kind: SolanaSystemTransfer, From: p.SenderAddress, To: p.PayTo(), Lamports: p.Amount.String(),
			}}
		}
		src, _ := chains.AssociatedTokenAddress(p.SenderAddress, p.Asset.Address, p.Asset.TokenProgram)
		dst, _ := chains.AssociatedTokenAddress(p.PayTo(), p.Asset.Address, p.Asset.TokenProgram)
		return Instructions{Solana: &SolanaInstruction{
			Kind: SolanaSPLTransferChecked, From: p.SenderAddress, To: p.PayTo(),
			Mint: p.Asset.Address, TokenProgram: p.Asset.TokenProgram, Decimals: p.Asset.Decimals,
			Amount: p.Amount.String(), SourceTokenAccount: src, DestinationTokenAccount: dst,
		}}
	}
	return Instructions{}
}
