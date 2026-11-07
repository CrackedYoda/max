package renames

import (
	"net/http"

	"github.com/max/max-core/internal/shared"
)

var (
	// ErrDefaultNotRenameable: default IDs are fixed. It shares its code with
	// maxids.ErrDefaultNotTransferable.
	ErrDefaultNotRenameable = shared.NewAppError("DEFAULT_ID_LOCKED", http.StatusConflict, "default MAX IDs cannot be renamed")
	ErrRenamesDisabled      = shared.NewAppError("RENAMES_DISABLED", http.StatusServiceUnavailable, "renames are not enabled on this server")
	ErrCooldown             = shared.NewAppError("RENAME_COOLDOWN", http.StatusConflict, "this MAX ID was renamed recently")
	ErrRenamePending        = shared.NewAppError("RENAME_PENDING", http.StatusConflict, "a rename for this MAX ID is already awaiting payment")
	ErrSameName             = shared.NewAppError("RENAME_SAME_NAME", http.StatusBadRequest, "new name is the same as the current name")
	ErrFeeOption            = shared.NewAppError("INVALID_FEE_OPTION", http.StatusBadRequest, "fee chain and asset must match one of the accepted fee options")
	ErrRequestNotFound      = shared.NewAppError("RENAME_NOT_FOUND", http.StatusNotFound, "rename request not found")
	ErrIDChanged            = shared.NewAppError("RENAME_CONFLICT", http.StatusConflict, "the MAX ID changed while the rename was pending")
)
