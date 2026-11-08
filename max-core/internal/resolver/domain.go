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
	IsDefault         bool
	ResolutionVersion int64
	// OwnerChangedAt is the raw timestamp; the service drops it once stale.
	OwnerChangedAt *time.Time
	Destinations   map[chains.Namespace]string
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
	IsDefault         bool
	Chain             chains.Chain
	Asset             Asset
	Address           string
	ResolutionVersion int64
	ResolvedAt        time.Time
	// OwnerChangedAt is set only when ownership changed in the last 30 days.
	OwnerChangedAt *time.Time
	Notes          []string
}

type ChainReach struct {
	Chain     chains.Chain
	Reachable bool
}
