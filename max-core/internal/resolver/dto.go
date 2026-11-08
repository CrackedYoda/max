package resolver

import (
	"time"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/maxids"
)

type assetResponse struct {
	Kind    string `json:"kind"`
	Address string `json:"address,omitempty"`
}

type resolveResponse struct {
	MaxID             string        `json:"maxId"`
	Handle            string        `json:"handle"`
	Type              string        `json:"type"`
	DisplayName       string        `json:"displayName"`
	IsDefault         bool          `json:"isDefault"`
	Chain             chains.Chain  `json:"chain"`
	Asset             assetResponse `json:"asset"`
	Address           string        `json:"address"`
	ResolutionVersion int64         `json:"resolutionVersion"`
	ResolvedAt        time.Time     `json:"resolvedAt"`
	OwnerChangedAt    string        `json:"ownerChangedAt,omitempty"`
	Notes             []string      `json:"notes"`
}

type reachResponse struct {
	Chain     chains.Chain `json:"chain"`
	Reachable bool         `json:"reachable"`
}

type settlementResponse struct {
	Chain chains.Chain  `json:"chain"`
	Asset assetResponse `json:"asset"`
}

type profileResponse struct {
	MaxID          string          `json:"maxId"`
	Name           string          `json:"name"`
	Handle         string          `json:"handle"`
	Type           string          `json:"type"`
	DisplayName    string          `json:"displayName"`
	IsDefault      bool            `json:"isDefault"`
	Description    string          `json:"description"`
	AvatarURL      string          `json:"avatarUrl"`
	Chains         []reachResponse `json:"chains"`
	OwnerChangedAt string          `json:"ownerChangedAt,omitempty"`
	// Settlement tells senders the recipient accepts quoted conversions into this asset.
	Settlement *settlementResponse `json:"settlement,omitempty"`
}

func toResolveResponse(r Resolution) resolveResponse {
	notes := r.Notes
	if notes == nil {
		notes = []string{}
	}
	return resolveResponse{
		MaxID: r.MaxID.String(), Handle: r.Handle, Type: r.Type, DisplayName: r.DisplayName, IsDefault: r.IsDefault,
		Chain: r.Chain, Asset: assetResponse{Kind: string(r.Asset.Kind), Address: r.Asset.Address},
		Address: r.Address, ResolutionVersion: r.ResolutionVersion, ResolvedAt: r.ResolvedAt, OwnerChangedAt: rfc3339(r.OwnerChangedAt), Notes: notes,
	}
}

func toProfileResponse(p Profile, reach []ChainReach) profileResponse {
	rr := make([]reachResponse, 0, len(reach))
	for _, c := range reach {
		rr = append(rr, reachResponse{Chain: c.Chain, Reachable: c.Reachable})
	}
	var st *settlementResponse
	if p.Settlement != nil {
		st = &settlementResponse{Chain: p.Settlement.Chain, Asset: assetResponse{Kind: string(p.Settlement.Asset.Kind), Address: p.Settlement.Asset.Address}}
	}
	return profileResponse{
		Settlement: st,
		MaxID:      p.MaxID.String(), Name: p.CanonicalName, Handle: maxids.Handle(p.CanonicalName),
		Type: p.Type, DisplayName: p.DisplayName, IsDefault: p.IsDefault, Description: p.Description,
		AvatarURL: p.AvatarURL, Chains: rr, OwnerChangedAt: rfc3339(p.OwnerChangedAt),
	}
}

func rfc3339(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
