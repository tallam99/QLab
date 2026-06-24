package pgstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/tallam99/qlab/backend/internal/store"
	"github.com/tallam99/qlab/backend/internal/store/pgstore/sqlcgen"
)

// Operator-tooling persistence (store.OperatorStore). These run cross-tenant and so
// are used only via a *pgstore.Store built over the elevated (BYPASS RLS)
// connection — never the per-request app pool. They write created_by/updated_by as
// NULL (the operator is not a user). See decision 0008.

// poolResourceKind is the one resource kind the MVP provisions.
const poolResourceKind = "VENT_HOOD"

// operatorActor returns a pointer to the operator sentinel actor, stamped on
// created_by/updated_by for every operator-written row (auditability, decision 0008).
func operatorActor() *uuid.UUID {
	id := store.OperatorActorID
	return &id
}

// CreateLabWorkspace creates a lab, a head + members, and a pool with resources in
// one transaction.
func (s *Store) CreateLabWorkspace(ctx context.Context, spec store.ProvisionSpec) (store.LabWorkspace, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return store.LabWorkspace{}, fmt.Errorf("begin provision tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	feature := strings.TrimSpace(spec.Feature)
	labID := uuid.New()
	labRow, err := q.CreateLab(ctx, sqlcgen.CreateLabParams{LabsID: labID, Name: feature, Actor: operatorActor()})
	if err != nil {
		return store.LabWorkspace{}, fmt.Errorf("create lab: %w", err)
	}

	members := make([]store.LabMember, 0, spec.MemberCount+1)
	head, err := createMember(ctx, q, labID, feature, store.LabRoleHead, "head")
	if err != nil {
		return store.LabWorkspace{}, err
	}
	members = append(members, head)
	for i := 1; i <= spec.MemberCount; i++ {
		m, err := createMember(ctx, q, labID, feature, store.LabRoleMember, fmt.Sprintf("member%d", i))
		if err != nil {
			return store.LabWorkspace{}, err
		}
		members = append(members, m)
	}

	poolID := uuid.New()
	poolRow, err := q.CreateResourcePool(ctx, sqlcgen.CreateResourcePoolParams{
		ResourcePoolsID: poolID, LabsID: labID, Kind: poolResourceKind, Name: feature + " pool", Actor: operatorActor(),
	})
	if err != nil {
		return store.LabWorkspace{}, fmt.Errorf("create pool: %w", err)
	}
	pool, err := resourcePoolFrom(poolRow.ResourcePoolsID, poolRow.LabsID, poolRow.Kind, poolRow.Name)
	if err != nil {
		return store.LabWorkspace{}, err
	}

	resources := make([]store.Resource, 0, spec.ResourceCount)
	for i := 1; i <= spec.ResourceCount; i++ {
		rRow, err := q.CreateResource(ctx, sqlcgen.CreateResourceParams{
			ResourcesID: uuid.New(), ResourcePoolsID: poolID, LabsID: labID,
			Kind: poolResourceKind, Name: fmt.Sprintf("Hood %d", i), Actor: operatorActor(),
		})
		if err != nil {
			return store.LabWorkspace{}, fmt.Errorf("create resource: %w", err)
		}
		res, err := resourceFrom(rRow.ResourcesID, rRow.ResourcePoolsID, rRow.LabsID, rRow.Kind, rRow.Name)
		if err != nil {
			return store.LabWorkspace{}, err
		}
		resources = append(resources, res)
	}

	if err := tx.Commit(ctx); err != nil {
		return store.LabWorkspace{}, fmt.Errorf("commit provision: %w", err)
	}
	return store.LabWorkspace{
		Lab:       store.Lab{ID: labRow.LabsID, Name: labRow.Name},
		Pool:      pool,
		Members:   members,
		Resources: resources,
	}, nil
}

// createMember creates an unlinked user (unique email derived from feature/role)
// and their membership, returning the domain member.
func createMember(ctx context.Context, q *sqlcgen.Queries, labID uuid.UUID, feature string, role store.LabRole, tag string) (store.LabMember, error) {
	userID := uuid.New()
	email := fmt.Sprintf("%s-%s-%s@qlab.dev", slug(feature), tag, uuid.NewString()[:8])
	uRow, err := q.CreateUserWithEmail(ctx, sqlcgen.CreateUserWithEmailParams{
		UsersID: userID, Email: email, Actor: operatorActor(),
		// Names fill from the provider on first login (LinkFirebaseUID only sets blanks),
		// so leave them blank here to mirror the real invite→login flow.
	})
	if err != nil {
		return store.LabMember{}, fmt.Errorf("create user: %w", err)
	}
	if err := q.CreateMembership(ctx, sqlcgen.CreateMembershipParams{
		LabsID: labID, UsersID: userID, Role: role.String(), Actor: operatorActor(),
	}); err != nil {
		return store.LabMember{}, fmt.Errorf("create membership: %w", err)
	}
	return store.LabMember{
		User: store.User{ID: uRow.UsersID, FirebaseUID: derefString(uRow.FirebaseUid), Email: uRow.Email, FirstName: uRow.FirstName, LastName: uRow.LastName},
		Role: role,
	}, nil
}

// ListLabs lists workspaces with counts, optionally filtered by name substring.
func (s *Store) ListLabs(ctx context.Context, featureFilter string) ([]store.LabSummary, error) {
	rows, err := s.q.ListLabsWithCounts(ctx, featureFilter)
	if err != nil {
		return nil, fmt.Errorf("list labs: %w", err)
	}
	out := make([]store.LabSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, store.LabSummary{
			Lab:           store.Lab{ID: r.LabsID, Name: r.Name},
			UserCount:     int(r.UserCount),
			ResourceCount: int(r.ResourceCount),
		})
	}
	return out, nil
}

// GetLabState returns a workspace's full state.
func (s *Store) GetLabState(ctx context.Context, labID uuid.UUID) (store.LabState, error) {
	labRow, err := s.q.LabByID(ctx, labID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.LabState{}, store.ErrNotFound
	}
	if err != nil {
		return store.LabState{}, fmt.Errorf("load lab: %w", err)
	}

	memberRows, err := s.q.ListLabMembers(ctx, labID)
	if err != nil {
		return store.LabState{}, fmt.Errorf("list members: %w", err)
	}
	members := make([]store.LabMember, 0, len(memberRows))
	for _, m := range memberRows {
		role, err := store.LabRoleString(m.Role)
		if err != nil {
			return store.LabState{}, fmt.Errorf("decode role %q: %w", m.Role, err)
		}
		members = append(members, store.LabMember{
			User: store.User{ID: m.UsersID, FirebaseUID: derefString(m.FirebaseUid), Email: m.Email, FirstName: m.FirstName, LastName: m.LastName},
			Role: role,
		})
	}

	poolRows, err := s.q.ListLabResourcePools(ctx, labID)
	if err != nil {
		return store.LabState{}, fmt.Errorf("list pools: %w", err)
	}
	pools := make([]store.ResourcePool, 0, len(poolRows))
	for _, p := range poolRows {
		pool, err := resourcePoolFrom(p.ResourcePoolsID, p.LabsID, p.Kind, p.Name)
		if err != nil {
			return store.LabState{}, err
		}
		pools = append(pools, pool)
	}

	resourceRows, err := s.q.ListLabResources(ctx, labID)
	if err != nil {
		return store.LabState{}, fmt.Errorf("list resources: %w", err)
	}
	resources := make([]store.Resource, 0, len(resourceRows))
	for _, r := range resourceRows {
		res, err := resourceFrom(r.ResourcesID, r.ResourcePoolsID, r.LabsID, r.Kind, r.Name)
		if err != nil {
			return store.LabState{}, err
		}
		resources = append(resources, res)
	}

	slotRows, err := s.q.ListLabSlots(ctx, labID)
	if err != nil {
		return store.LabState{}, fmt.Errorf("list slots: %w", err)
	}
	slots := make([]store.Slot, 0, len(slotRows))
	for _, sl := range slotRows {
		slot, err := slotFromLabSlots(sl)
		if err != nil {
			return store.LabState{}, err
		}
		slots = append(slots, slot)
	}

	return store.LabState{
		Lab:       store.Lab{ID: labRow.LabsID, Name: labRow.Name},
		Members:   members,
		Pools:     pools,
		Resources: resources,
		Slots:     slots,
	}, nil
}

// DeleteLab deletes a workspace (cascade). ErrNotFound when absent.
func (s *Store) DeleteLab(ctx context.Context, labID uuid.UUID) error {
	rows, err := s.q.DeleteLab(ctx, labID)
	if err != nil {
		return fmt.Errorf("delete lab: %w", err)
	}
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}

// UserByID loads a user by id. ErrNotFound when absent.
func (s *Store) UserByID(ctx context.Context, userID uuid.UUID) (store.User, error) {
	row, err := s.q.UserByID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, store.ErrNotFound
	}
	if err != nil {
		return store.User{}, fmt.Errorf("load user by id: %w", err)
	}
	return store.User{ID: row.UsersID, FirebaseUID: derefString(row.FirebaseUid), Email: row.Email, FirstName: row.FirstName, LastName: row.LastName}, nil
}

// --- converters shared by the operator reads ---

func resourcePoolFrom(id, labID uuid.UUID, kindLabel, name string) (store.ResourcePool, error) {
	kind, err := store.ResourceKindString(kindLabel)
	if err != nil {
		return store.ResourcePool{}, fmt.Errorf("decode resource kind %q: %w", kindLabel, err)
	}
	return store.ResourcePool{ID: id, LabID: labID, Kind: kind, Name: name}, nil
}

func resourceFrom(id, poolID, labID uuid.UUID, kindLabel, name string) (store.Resource, error) {
	kind, err := store.ResourceKindString(kindLabel)
	if err != nil {
		return store.Resource{}, fmt.Errorf("decode resource kind %q: %w", kindLabel, err)
	}
	return store.Resource{ID: id, ResourcePoolID: poolID, LabID: labID, Kind: kind, Name: name}, nil
}

func slotFromLabSlots(r sqlcgen.ListLabSlotsRow) (store.Slot, error) {
	st, err := decodeStatus(r.Status)
	if err != nil {
		return store.Slot{}, err
	}
	return store.Slot{
		ID: r.SlotsID, LabID: r.LabsID, UserID: r.UsersID, ResourcePoolID: r.ResourcePoolsID,
		ResourceID: derefUUID(r.ResourcesID), Priority: r.SlotPriority, Status: st,
		DesiredStart: r.DesiredStart, LookaheadMinutes: r.Lookahead, DurationMinutes: r.Duration,
		CommittedStart: derefTime(r.CommittedStart), ActualStart: derefTime(r.ActualStart), Note: r.Note,
	}, nil
}

// slug lowercases s and replaces any run of non-alphanumerics with a single '-',
// trimming leading/trailing '-', so it forms a valid email local-part fragment.
func slug(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "lab"
	}
	return out
}
