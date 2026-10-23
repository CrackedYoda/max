package resolver

import "context"

type Repository interface {
	// GetProfile returns ErrMaxIDNotFound for unknown or inactive IDs.
	GetProfile(ctx context.Context, canonical string) (Profile, error)
}

type Service interface {
	Profile(ctx context.Context, name string) (Profile, []ChainReach, error)
	Resolve(ctx context.Context, name, chain, asset string) (Resolution, error)
	// Settlement returns the recipient's enabled conversion preference, or nil.
	Settlement(ctx context.Context, name string) (*Settlement, error)
}
