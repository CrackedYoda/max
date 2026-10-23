package resolver

import (
	"time"

	"github.com/google/uuid"

	"github.com/max/max-core/internal/chains"
)

type Profile struct {
	MaxID             uuid.UUID
	CanonicalName     string
	Type              string
	DisplayName       string
	Description       string
	AvatarURL         string
	ResolutionVersion int64
	Destinations      map[chains.Namespace]string
	// Settlement is set only when the owner has an enabled conversion preference.
	Settlement *Settlement
}

type Settlement struct {
	Chain chains.Chain
	Asset Asset
}

// AssetKind distinguishes a chain's native coin from a token identified by its
// contract (EVM) or mint (Solana). Symbols are never used to identify assets.
type AssetKind string

const (
	AssetNative AssetKind = "native"
	AssetToken  AssetKind = "token"
)

type Asset struct {
	Kind AssetKind
	// Contract or mint address; empty for native.
	Address string
}

type Resolution struct {
	MaxID             uuid.UUID
	Handle            string
	Type              string
	DisplayName       string
	Chain             chains.Chain
	Asset             Asset
	Address           string
	ResolutionVersion int64
	ResolvedAt        time.Time
	Notes             []string
}

type ChainReach struct {
	Chain     chains.Chain
	Reachable bool
}
