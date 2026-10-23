package payments

var transitions = map[Status][]Status{
	StatusCreated:                 {StatusQuoted, StatusAwaitingSenderSignature, StatusExpired, StatusFailed},
	StatusQuoted:                  {StatusAwaitingSenderSignature, StatusSubmitted, StatusExpired},
	StatusAwaitingSenderSignature: {StatusSubmitted, StatusExpired},
	// A transaction signed just before expiry must still be recordable, or the
	// sender's funds move with no matching payment record.
	StatusExpired:   {StatusSubmitted},
	StatusSubmitted: {StatusConfirming, StatusCompleted, StatusFailed, StatusManualReview, StatusRefundPending},
	// Cross-chain routes can end in a provider refund instead of delivery.
	StatusConfirming:    {StatusCompleted, StatusFailed, StatusSubmitted, StatusManualReview, StatusRefundPending, StatusRefunded},
	StatusFailed:        {StatusRefundPending},
	StatusRefundPending: {StatusRefunded, StatusManualReview},
	StatusManualReview:  {StatusCompleted, StatusFailed},
}

func CanTransition(from, to Status) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

func (s Status) Terminal() bool {
	return s == StatusCompleted || s == StatusRefunded
}
