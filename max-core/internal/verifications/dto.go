package verifications

import (
	"time"

	"github.com/max/max-core/internal/maxids"
)

type claimRequest struct {
	Name        string `json:"name" binding:"required,max=64"`
	DisplayName string `json:"displayName" binding:"max=64"`
}

type noteRequest struct {
	Note string `json:"note" binding:"max=280"`
}

type verificationResponse struct {
	ID           string     `json:"id"`
	Provider     string     `json:"provider"`
	Username     string     `json:"username"`
	DisplayName  string     `json:"displayName"`
	VerifiedAt   time.Time  `json:"verifiedAt"`
	RevokedAt    *time.Time `json:"revokedAt,omitempty"`
	RevokeReason string     `json:"revokeReason,omitempty"`
}

type claimResponse struct {
	ID            string     `json:"id"`
	Handle        string     `json:"handle"`
	Status        string     `json:"status"`
	XUsername     string     `json:"xUsername"`
	MaxID         string     `json:"maxId,omitempty"`
	VerifiedUntil *time.Time `json:"verifiedUntil,omitempty"`
	ReviewNote    string     `json:"reviewNote,omitempty"`
	DecidedAt     *time.Time `json:"decidedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
}

func toVerificationResponse(v Verification) verificationResponse {
	return verificationResponse{
		ID: v.ID.String(), Provider: v.Provider, Username: v.Username, DisplayName: v.DisplayName,
		VerifiedAt: v.VerifiedAt, RevokedAt: v.RevokedAt, RevokeReason: v.RevokeReason,
	}
}

func toClaimResponse(c Claim) claimResponse {
	r := claimResponse{
		ID: c.ID.String(), Handle: maxids.Handle(c.CanonicalName), Status: string(c.Status), XUsername: c.XUsername,
		VerifiedUntil: c.VerifiedUntil, ReviewNote: c.ReviewNote, DecidedAt: c.DecidedAt, CreatedAt: c.CreatedAt,
	}
	if c.MaxID != nil {
		r.MaxID = c.MaxID.String()
	}
	return r
}

func toClaimResponses(cs []Claim) []claimResponse {
	out := make([]claimResponse, 0, len(cs))
	for _, c := range cs {
		out = append(out, toClaimResponse(c))
	}
	return out
}
