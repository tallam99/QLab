// Canonical request-header names the Connect API reads. These mirror the
// backend's `internal/api/auth.go` constants — the Authorization bearer token
// identifies the caller and X-QLab-Lab selects which of their labs they act in
// (RLS is fail-closed, so the lab must be sent explicitly — decision 0006).
export const HEADER_AUTHORIZATION = "Authorization";
export const HEADER_SELECTED_LAB = "X-QLab-Lab";
