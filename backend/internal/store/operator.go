package store

import (
	"context"

	"github.com/google/uuid"
)

// OperatorStore is the persistence the staging/local operator tooling needs
// (decision 0008). It is a SEPARATE interface from Store because its operations are
// inherently cross-tenant admin work (create a whole lab, list every workspace) —
// the kind of thing the per-request, RLS-scoped Store deliberately cannot do. Its
// implementation runs over an elevated (BYPASS row-level-security) connection, so
// it must never be wired in production. The same *pgstore.Store satisfies both
// interfaces; only the connection differs.
type OperatorStore interface {
	// CreateLabWorkspace creates a demo workspace atomically: a lab, a head + the
	// requested members, and one pool with the requested resources. Users are created
	// unlinked (no Firebase identity); first token use links them.
	CreateLabWorkspace(ctx context.Context, spec ProvisionSpec) (LabWorkspace, error)
	// ListLabs lists workspaces with member/resource counts, optionally filtered to
	// those whose name contains featureFilter (empty = all).
	ListLabs(ctx context.Context, featureFilter string) ([]LabSummary, error)
	// GetLabState returns a workspace's full state. ErrNotFound if the lab is absent.
	GetLabState(ctx context.Context, labID uuid.UUID) (LabState, error)
	// DeleteLab deletes a workspace (cascade). ErrNotFound if it does not exist. The
	// global users rows are left (they may belong to other labs).
	DeleteLab(ctx context.Context, labID uuid.UUID) error
	// UserByID loads a user by id (for minting a token for a provisioned user).
	// ErrNotFound if absent.
	UserByID(ctx context.Context, userID uuid.UUID) (User, error)
}

// ProvisionSpec describes a workspace to create.
type ProvisionSpec struct {
	// Feature labels the workspace; stored as the lab name.
	Feature string
	// MemberCount is how many MEMBER users to create beside the single HEAD.
	MemberCount int
	// ResourceCount is how many interchangeable resources the pool holds.
	ResourceCount int
}

// LabMember pairs a user with their role in a lab.
type LabMember struct {
	User User
	Role LabRole
}

// LabWorkspace is a freshly provisioned workspace.
type LabWorkspace struct {
	Lab       Lab
	Pool      ResourcePool
	Members   []LabMember
	Resources []Resource
}

// LabSummary is a workspace in a listing.
type LabSummary struct {
	Lab           Lab
	UserCount     int
	ResourceCount int
}

// LabState is a workspace's full state (the operator state-export).
type LabState struct {
	Lab       Lab
	Members   []LabMember
	Pools     []ResourcePool
	Resources []Resource
	Slots     []Slot
}
