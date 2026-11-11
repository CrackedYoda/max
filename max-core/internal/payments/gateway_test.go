package payments

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/evmrpc"
	"github.com/max/max-core/internal/solanarpc"
)

const (
	evmSender    = "0x1111111111111111111111111111111111111111"
	evmRecipient = "0x2222222222222222222222222222222222222222"
	evmOther     = "0x3333333333333333333333333333333333333333"
	usdcBase     = "0x833589fcd6edb6e08f4c7c32d4f71b54bda02913"
	evmTxHash    = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	solSender    = "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM"
	solRecipient = "4Nd1mBQtrMJVYVfKf2PJy9NZUZdTAsp7D4xWLs4gDB4T"
	bonkMint     = "DezXAZ8z7PnrnRJjz3wXBoRgixCa6xjnB7YaB1pPB263"
	solSig       = "5VERv8NMvzbJMEkV8xnrLkEaWRtSz9CosKDYjCJjBRnbJLgp8uirBgmQpjKhoR4tjF3ZpRzrFmBV6UjKdiSZkQUW"
)

type fakeEVM struct {
	chainID   int64
	head      int64
	receipt   *evmrpc.Receipt
	tx        *evmrpc.Transaction
	blockTime int64
	hasCode   bool
	decimals  int
}

func (f *fakeEVM) ChainID(context.Context) (int64, error)     { return f.chainID, nil }
func (f *fakeEVM) BlockNumber(context.Context) (int64, error) { return f.head, nil }
func (f *fakeEVM) Receipt(context.Context, string) (*evmrpc.Receipt, error) {
	return f.receipt, nil
}
func (f *fakeEVM) Transaction(context.Context, string) (*evmrpc.Transaction, error) {
	return f.tx, nil
}
func (f *fakeEVM) BlockTimestamp(context.Context, string) (int64, error) { return f.blockTime, nil }
func (f *fakeEVM) HasCode(context.Context, string) (bool, error)         { return f.hasCode, nil }
func (f *fakeEVM) ERC20Decimals(context.Context, string) (int, error)    { return f.decimals, nil }

type fakeSolana struct {
	tx       *solanarpc.Transaction
	status   string
	accounts map[string]*solanarpc.AccountInfo
}

func (f *fakeSolana) Transaction(context.Context, string) (*solanarpc.Transaction, error) {
	return f.tx, nil
}
func (f *fakeSolana) ConfirmationStatus(context.Context, string) (string, error) {
	return f.status, nil
}
func (f *fakeSolana) AccountInfo(_ context.Context, addr string) (*solanarpc.AccountInfo, error) {
	return f.accounts[addr], nil
}

func mustChain(key string) chains.Chain {
	c, _ := chains.Lookup(key)
	return c
}

func evmPayment(kind AssetKind, amount int64) Payment {
	p := Payment{
		ID: uuid.New(), SenderAddress: evmSender, RecipientAddress: evmRecipient,
		Chain: mustChain("base"), Amount: big.NewInt(amount), TxHash: evmTxHash,
		RequiredConfirmations: 2, CreatedAt: time.Unix(1_700_000_000, 0),
	}
	if kind == AssetNative {
		p.Asset = Asset{Kind: AssetNative, Decimals: 18}
	} else {
		p.Asset = Asset{Kind: AssetToken, Address: usdcBase, Decimals: 6}
	}
	return p
}

func transferLog(token, from, to string, amount int64) evmrpc.Log {
	return evmrpc.Log{
		Address: token,
		Topics:  []string{chains.ERC20TransferTopic, chains.AddressTopic(from), chains.AddressTopic(to)},
		Data:    fmt.Sprintf("0x%064x", amount),
	}
}

func baseEVM() *fakeEVM {
	return &fakeEVM{
		chainID: 8453, head: 101, blockTime: 1_700_000_100,
		receipt: &evmrpc.Receipt{Status: "0x1", BlockNumber: "0x64"}, // block 100
		tx:      &evmrpc.Transaction{From: evmSender, To: evmRecipient, Value: "0x3e8"},
	}
}

func TestObserveEVM(t *testing.T) {
	tests := []struct {
		name    string
		kind    AssetKind
		mutate  func(*fakeEVM)
		want    Observation
		wantErr bool
	}{
		{"native exact match, 2 confirmations is final", AssetNative, nil,
			Observation{Found: true, Confirmations: 2, Final: true}, false},
		{"native not yet final", AssetNative, func(f *fakeEVM) { f.head = 100 },
			Observation{Found: true, Confirmations: 1}, false},
		{"pending", AssetNative, func(f *fakeEVM) { f.receipt = nil },
			Observation{}, false},
		{"reverted", AssetNative, func(f *fakeEVM) { f.receipt.Status = "0x0" },
			Observation{Found: true, Reverted: true}, false},
		{"wrong recipient", AssetNative, func(f *fakeEVM) { f.tx.To = evmOther },
			Observation{Found: true, Mismatch: "sent to a different address than the resolved recipient"}, false},
		{"wrong amount", AssetNative, func(f *fakeEVM) { f.tx.Value = "0x3e7" },
			Observation{Found: true, Mismatch: "transferred value differs from the payment amount"}, false},
		{"wrong sender", AssetNative, func(f *fakeEVM) { f.tx.From = evmOther },
			Observation{Found: true, Mismatch: "sent from a different address than the payment's sender"}, false},
		{"old transaction reused", AssetNative, func(f *fakeEVM) { f.blockTime = 1_699_000_000 },
			Observation{Found: true, Mismatch: "transaction was mined before this payment intent was created"}, false},
		{"rpc serves the wrong chain", AssetNative, func(f *fakeEVM) { f.chainID = 1 },
			Observation{}, true},
		{"erc20 transfer log match", AssetToken, func(f *fakeEVM) {
			f.receipt.Logs = []evmrpc.Log{transferLog(usdcBase, evmSender, evmRecipient, 1000)}
		}, Observation{Found: true, Confirmations: 2, Final: true}, false},
		{"erc20 checksummed log address still matches", AssetToken, func(f *fakeEVM) {
			f.receipt.Logs = []evmrpc.Log{transferLog(chains.ChecksumEVMAddress(usdcBase), evmSender, evmRecipient, 1000)}
		}, Observation{Found: true, Confirmations: 2, Final: true}, false},
		{"erc20 from a fake token contract", AssetToken, func(f *fakeEVM) {
			f.receipt.Logs = []evmrpc.Log{transferLog(evmOther, evmSender, evmRecipient, 1000)}
		}, Observation{Found: true, Mismatch: "no matching token Transfer from sender to recipient for the exact amount"}, false},
		{"erc20 to someone else", AssetToken, func(f *fakeEVM) {
			f.receipt.Logs = []evmrpc.Log{transferLog(usdcBase, evmSender, evmOther, 1000)}
		}, Observation{Found: true, Mismatch: "no matching token Transfer from sender to recipient for the exact amount"}, false},
		// security-review S02: only the payment's own transfer failing is a
		// revert; an unrelated failed hash must not read as one.
		{"reverted from another sender", AssetNative, func(f *fakeEVM) { f.receipt.Status = "0x0"; f.tx.From = evmOther },
			Observation{Found: true, Mismatch: "failed transaction is not this payment's transfer: sent from a different address than the payment's sender"}, false},
		{"reverted to another recipient", AssetNative, func(f *fakeEVM) { f.receipt.Status = "0x0"; f.tx.To = evmOther },
			Observation{Found: true, Mismatch: "failed transaction is not this payment's transfer: sent to a different address than the resolved recipient"}, false},
		{"reverted before the intent existed", AssetNative, func(f *fakeEVM) { f.receipt.Status = "0x0"; f.blockTime = 1_699_000_000 },
			Observation{Found: true, Mismatch: "transaction was mined before this payment intent was created"}, false},
		{"erc20 transfer call reverted", AssetToken, func(f *fakeEVM) {
			f.receipt.Status = "0x0"
			f.tx = &evmrpc.Transaction{From: evmSender, To: usdcBase, Value: "0x0", Input: chains.EncodeERC20Transfer(strings.ToLower(evmRecipient), big.NewInt(1000))}
		}, Observation{Found: true, Reverted: true}, false},
		{"erc20 unrelated call reverted", AssetToken, func(f *fakeEVM) {
			f.receipt.Status = "0x0"
			f.tx = &evmrpc.Transaction{From: evmSender, To: usdcBase, Value: "0x0", Input: chains.EncodeERC20Transfer(strings.ToLower(evmOther), big.NewInt(1000))}
		}, Observation{Found: true, Mismatch: "failed transaction is not this payment's transfer: not a transfer call to the recipient for the exact amount"}, false},
		{"erc20 removed (reorged) log ignored", AssetToken, func(f *fakeEVM) {
			l := transferLog(usdcBase, evmSender, evmRecipient, 1000)
			l.Removed = true
			f.receipt.Logs = []evmrpc.Log{l}
		}, Observation{Found: true, Mismatch: "no matching token Transfer from sender to recipient for the exact amount"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := baseEVM()
			if tt.mutate != nil {
				tt.mutate(f)
			}
			g := newGateway(map[string]EVMClient{"base": f}, nil)
			got, err := g.Observe(context.Background(), evmPayment(tt.kind, 1000))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func parsedIx(programID, program string, v any) solanarpc.ParsedInstruction {
	b, _ := json.Marshal(v)
	return solanarpc.ParsedInstruction{ProgramID: programID, Program: program, Parsed: b}
}

func solTx(ixs ...solanarpc.ParsedInstruction) *solanarpc.Transaction {
	bt := int64(1_700_000_100)
	tx := &solanarpc.Transaction{BlockTime: &bt}
	tx.Meta = &struct {
		Err               json.RawMessage               `json:"err"`
		InnerInstructions []solanarpc.InnerInstructions `json:"innerInstructions"`
	}{Err: json.RawMessage("null")}
	tx.Transaction.Message.Instructions = ixs
	return tx
}

func solPayment(kind AssetKind, amount int64) Payment {
	p := Payment{
		ID: uuid.New(), SenderAddress: solSender, RecipientAddress: solRecipient,
		Chain: mustChain("solana"), Amount: big.NewInt(amount), TxHash: solSig,
		RequiredConfirmations: 1, CreatedAt: time.Unix(1_700_000_000, 0),
	}
	if kind == AssetNative {
		p.Asset = Asset{Kind: AssetNative, Decimals: 9}
	} else {
		p.Asset = Asset{Kind: AssetToken, Address: bonkMint, Decimals: 5, TokenProgram: chains.SPLTokenProgram}
	}
	return p
}

func TestObserveSolana(t *testing.T) {
	destATA, _ := chains.AssociatedTokenAddress(solRecipient, bonkMint, chains.SPLTokenProgram)
	wrongATA, _ := chains.AssociatedTokenAddress(solSender, bonkMint, chains.SPLTokenProgram)

	systemTransfer := func(src, dst string, lamports int64) solanarpc.ParsedInstruction {
		return parsedIx(chains.SystemProgram, "system", map[string]any{
			"type": "transfer", "info": map[string]any{"source": src, "destination": dst, "lamports": lamports},
		})
	}
	splChecked := func(dst, authority, mint, amount string) solanarpc.ParsedInstruction {
		return parsedIx(chains.SPLTokenProgram, "spl-token", map[string]any{
			"type": "transferChecked", "info": map[string]any{
				"destination": dst, "authority": authority, "mint": mint,
				"tokenAmount": map[string]any{"amount": amount},
			},
		})
	}

	tests := []struct {
		name   string
		kind   AssetKind
		tx     *solanarpc.Transaction
		status string
		want   Observation
	}{
		{"SOL finalized", AssetNative, solTx(systemTransfer(solSender, solRecipient, 100_000_000)), "finalized",
			Observation{Found: true, Confirmations: 1, Final: true}},
		{"SOL confirmed not finalized", AssetNative, solTx(systemTransfer(solSender, solRecipient, 100_000_000)), "confirmed",
			Observation{Found: true}},
		{"SOL lamports above 2^53 match exactly", AssetNative, solTx(systemTransfer(solSender, solRecipient, 9_007_199_254_740_993)), "finalized",
			Observation{Found: true, Confirmations: 1, Final: true}},
		{"SOL off by one lamport above 2^53", AssetNative, solTx(systemTransfer(solSender, solRecipient, 9_007_199_254_740_993)), "finalized",
			Observation{Found: true, Mismatch: "no SOL transfer from sender to recipient for the exact amount"}},
		{"SOL wrong destination", AssetNative, solTx(systemTransfer(solSender, solSender, 100_000_000)), "finalized",
			Observation{Found: true, Mismatch: "no SOL transfer from sender to recipient for the exact amount"}},
		{"not visible yet", AssetNative, nil, "", Observation{}},
		{"BONK transferChecked to recipient ATA", AssetToken, solTx(splChecked(destATA, solSender, bonkMint, "1000")), "finalized",
			Observation{Found: true, Confirmations: 1, Final: true}},
		{"BONK as inner instruction (via a program)", AssetToken, func() *solanarpc.Transaction {
			tx := solTx()
			tx.Meta.InnerInstructions = []solanarpc.InnerInstructions{{Instructions: []solanarpc.ParsedInstruction{splChecked(destATA, solSender, bonkMint, "1000")}}}
			return tx
		}(), "finalized", Observation{Found: true, Confirmations: 1, Final: true}},
		{"token sent to the wrong ATA", AssetToken, solTx(splChecked(wrongATA, solSender, bonkMint, "1000")), "finalized",
			Observation{Found: true, Mismatch: "no token transfer from sender to the recipient's token account for the exact amount"}},
		{"token signed by someone else", AssetToken, solTx(splChecked(destATA, solRecipient, bonkMint, "1000")), "finalized",
			Observation{Found: true, Mismatch: "no token transfer from sender to the recipient's token account for the exact amount"}},
		{"wrong mint", AssetToken, solTx(splChecked(destATA, solSender, "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v", "1000")), "finalized",
			Observation{Found: true, Mismatch: "no token transfer from sender to the recipient's token account for the exact amount"}},
		{"failed transaction", AssetNative, func() *solanarpc.Transaction {
			tx := solTx(systemTransfer(solSender, solRecipient, 100_000_000))
			tx.Meta.Err = json.RawMessage(`{"InstructionError":[0,"Custom"]}`)
			return tx
		}(), "finalized", Observation{Found: true, Reverted: true}},
		{"failed transaction that is not this payment's", AssetNative, func() *solanarpc.Transaction {
			tx := solTx(systemTransfer(solRecipient, solSender, 100_000_000))
			tx.Meta.Err = json.RawMessage(`{"InstructionError":[0,"Custom"]}`)
			return tx
		}(), "finalized", Observation{Found: true, Mismatch: "failed transaction is not this payment's transfer: no SOL transfer from sender to recipient for the exact amount"}},
	}
	amountOverride := map[string]int64{
		"SOL lamports above 2^53 match exactly": 9_007_199_254_740_993,
		"SOL off by one lamport above 2^53":     9_007_199_254_740_992,
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeSolana{tx: tt.tx, status: tt.status}
			g := newGateway(nil, map[string]SolanaClient{"solana": f})
			amount := int64(100_000_000)
			if tt.kind == AssetToken {
				amount = 1000
			}
			if a, ok := amountOverride[tt.name]; ok {
				amount = a
			}
			got, err := g.Observe(context.Background(), solPayment(tt.kind, amount))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestTokenInfo(t *testing.T) {
	ctx := context.Background()

	evm := baseEVM()
	evm.hasCode, evm.decimals = true, 6
	g := newGateway(map[string]EVMClient{"base": evm}, nil)
	if info, err := g.TokenInfo(ctx, mustChain("base"), usdcBase); err != nil || info.Decimals != 6 {
		t.Fatalf("erc20: %+v %v", info, err)
	}
	evm.hasCode = false
	if _, err := g.TokenInfo(ctx, mustChain("base"), usdcBase); err == nil || !strings.Contains(err.Error(), "no contract") {
		t.Fatalf("EOA accepted as token: %v", err)
	}

	mintInfo := &solanarpc.AccountInfo{Owner: chains.SPLTokenProgram}
	mintInfo.Data.Parsed = &struct {
		Type string          `json:"type"`
		Info json.RawMessage `json:"info"`
	}{Type: "mint", Info: json.RawMessage(`{"decimals":5}`)}
	sol := &fakeSolana{accounts: map[string]*solanarpc.AccountInfo{bonkMint: mintInfo}}
	g = newGateway(nil, map[string]SolanaClient{"solana": sol})
	info, err := g.TokenInfo(ctx, mustChain("solana"), bonkMint)
	if err != nil || info.Decimals != 5 || info.TokenProgram != chains.SPLTokenProgram {
		t.Fatalf("spl: %+v %v", info, err)
	}
	if _, err := g.TokenInfo(ctx, mustChain("solana"), solRecipient); err == nil {
		t.Fatal("wallet address accepted as mint")
	}
}
