package maxids

import (
	"time"

	"github.com/max/max-core/internal/chains"
	"github.com/max/max-core/internal/shared"
)

type claimRequest struct {
	Name        string `json:"name" binding:"required,max=64"`
	Type        string `json:"type" binding:"omitempty,oneof=human agent"`
	DisplayName string `json:"displayName" binding:"max=64"`
	// Nonce and Signature are a claim challenge (POST /v1/wallets/challenge,
	// purpose "claim") signed by one of the caller's wallets.
	Nonce     string `json:"nonce" binding:"omitempty,len=32,hexadecimal"`
	Signature string `json:"signature" binding:"max=256"`
}

type profileRequest struct {
	DisplayName string `json:"displayName" binding:"max=64"`
	Description string `json:"description" binding:"max=280"`
	AvatarURL   string `json:"avatarUrl" binding:"max=512"`
}

// destinationRequest names either a linked wallet or a pasted address.
type destinationRequest struct {
	WalletID  string `json:"walletId" binding:"omitempty,uuid"`
	Address   string `json:"address" binding:"max=128"`
	Confirmed bool   `json:"confirmed"`
}

func (r destinationRequest) validate() error {
	if (r.WalletID == "") == (r.Address == "") {
		return shared.ErrValidation.WithMessage("send exactly one of walletId or address")
	}
	return nil
}

type settlementRequest struct {
	Chain   string `json:"chain" binding:"required,max=32"`
	Asset   string `json:"asset" binding:"max=128"`
	Enabled *bool  `json:"enabled"`
}

type settlementResponse struct {
	Chain     string    `json:"chain"`
	Asset     string    `json:"asset"`
	Enabled   bool      `json:"enabled"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type availabilityResponse struct {
	Name        string `json:"name"`
	Handle      string `json:"handle"`
	Available   bool   `json:"available"`
	Reason      string `json:"reason,omitempty"`
	Detail      string `json:"detail,omitempty"`
	ClaimPolicy string `json:"claimPolicy,omitempty"`
	XHandle     string `json:"xHandle,omitempty"`
}

type destinationResponse struct {
	Namespace string `json:"namespace"`
	// Chain is the chain key of a per-chain override; null for a namespace default.
	Chain     *string   `json:"chain"`
	Address   string    `json:"address"`
	Source    string    `json:"source"`
	Verified  bool      `json:"verified"`
	WalletID  *string   `json:"walletId"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type maxIDResponse struct {
	ID                string                `json:"id"`
	Name              string                `json:"name"`
	Handle            string                `json:"handle"`
	Type              string                `json:"type"`
	Status            string                `json:"status"`
	IsDefault         bool                  `json:"isDefault"`
	DisplayName       string                `json:"displayName"`
	Description       string                `json:"description"`
	AvatarURL         string                `json:"avatarUrl"`
	ResolutionVersion int64                 `json:"resolutionVersion"`
	RenameAvailableAt time.Time             `json:"renameAvailableAt"`
	CreatedAt         time.Time             `json:"createdAt"`
	Destinations      []destinationResponse `json:"destinations"`
	Settlement        *settlementResponse   `json:"settlement"`
}

func toAvailabilityResponse(a Availability) availabilityResponse {
	return availabilityResponse{
		Name: a.CanonicalName, Handle: a.Handle, Available: a.Available,
		Reason: a.Reason, Detail: a.Detail, ClaimPolicy: string(a.ClaimPolicy), XHandle: a.XHandle,
	}
}

func toMaxIDResponse(m MaxID) maxIDResponse {
	dests := make([]destinationResponse, 0, len(m.Destinations))
	for _, d := range m.Destinations {
		r := destinationResponse{
			Namespace: string(d.Namespace), Source: "pasted",
			Address: chains.DisplayAddress(d.Namespace, d.Address), UpdatedAt: d.UpdatedAt,
		}
		if d.ChainKey != "" {
			k := d.ChainKey
			r.Chain = &k
		}
		if d.WalletID != nil {
			id := d.WalletID.String()
			r.Source, r.Verified, r.WalletID = "wallet", true, &id
		}
		dests = append(dests, r)
	}
	var settlement *settlementResponse
	if st := m.Settlement; st != nil {
		asset := "native"
		if st.Asset != "" {
			c, _ := chains.Lookup(st.Chain)
			asset = chains.DisplayAddress(c.Namespace, st.Asset)
		}
		settlement = &settlementResponse{Chain: st.Chain, Asset: asset, Enabled: st.Enabled, UpdatedAt: st.UpdatedAt}
	}
	return maxIDResponse{
		Settlement: settlement,
		ID:         m.ID.String(), Name: m.CanonicalName, Handle: m.Handle(), Type: string(m.Type),
		Status: m.Status, IsDefault: m.IsDefault, DisplayName: m.DisplayName, Description: m.Description,
		AvatarURL: m.AvatarURL, ResolutionVersion: m.ResolutionVersion,
		RenameAvailableAt: m.RenameAvailableAt, CreatedAt: m.CreatedAt, Destinations: dests,
	}
}
