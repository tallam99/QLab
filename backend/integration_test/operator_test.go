//go:build integration

package integrationtest

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	devv1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/dev/v1"
	v1 "github.com/tallam99/qlab/backend/internal/protogen/qlab/v1"
	"github.com/tallam99/qlab/backend/internal/store"
)

// TestOperatorProvisionAndMint: the operator provisions a workspace, then mints a
// token for one of its users and uses it to make a real authenticated API call —
// the whole staging dev flow end to end (provision → impersonate → act).
func (s *IntegrationSuite) TestOperatorProvisionAndMint() {
	t := s.T()
	ctx := context.Background()
	op := h.operatorClient()

	prov, err := op.ProvisionLab(ctx, connect.NewRequest(&devv1.ProvisionLabRequest{
		Feature: "search", MemberCount: 2, ResourceCount: 3,
	}))
	require.NoError(t, err)
	res := prov.Msg
	assert.Equal(t, "search", res.GetLab().GetName())
	require.Len(t, res.GetMembers(), 3, "1 head + 2 members")
	require.Len(t, res.GetResources(), 3)
	assert.Equal(t, v1.LabRole_LAB_ROLE_HEAD, res.GetMembers()[0].GetRole())

	// Operator-written rows are attributed to the operator sentinel actor, not NULL.
	var createdBy uuid.UUID
	require.NoError(t, h.admin.QueryRow(ctx, `SELECT created_by FROM labs WHERE labs_id = $1`, res.GetLab().GetId()).Scan(&createdBy))
	assert.Equal(t, store.OperatorActorID, createdBy, "lab.created_by is the operator sentinel actor")

	// Mint a token for a member and act as them against the public API.
	member := res.GetMembers()[1]
	assert.Equal(t, v1.LabRole_LAB_ROLE_MEMBER, member.GetRole())
	mint, err := op.MintToken(ctx, connect.NewRequest(&devv1.MintTokenRequest{UserId: member.GetUser().GetId()}))
	require.NoError(t, err)
	require.NotEmpty(t, mint.Msg.GetIdToken())

	_, err = h.bearerClient(mint.Msg.GetIdToken(), res.GetLab().GetId()).
		ListSlots(ctx, connect.NewRequest(&v1.ListSlotsRequest{ResourcePoolId: res.GetPool().GetId()}))
	require.NoError(t, err, "minted token should authenticate against the provisioned lab")
}

// TestOperatorListAndGet: ListLabs filters by feature; GetLab returns a workspace's
// full state.
func (s *IntegrationSuite) TestOperatorListAndGet() {
	t := s.T()
	ctx := context.Background()
	op := h.operatorClient()

	alpha, err := op.ProvisionLab(ctx, connect.NewRequest(&devv1.ProvisionLabRequest{Feature: "alpha", MemberCount: 1, ResourceCount: 1}))
	require.NoError(t, err)
	_, err = op.ProvisionLab(ctx, connect.NewRequest(&devv1.ProvisionLabRequest{Feature: "beta", MemberCount: 0, ResourceCount: 2}))
	require.NoError(t, err)

	// Filter by feature returns only the matching workspace.
	filtered, err := op.ListLabs(ctx, connect.NewRequest(&devv1.ListLabsRequest{Feature: "alpha"}))
	require.NoError(t, err)
	require.Len(t, filtered.Msg.GetLabs(), 1)
	assert.Equal(t, "alpha", filtered.Msg.GetLabs()[0].GetLab().GetName())
	assert.EqualValues(t, 2, filtered.Msg.GetLabs()[0].GetUserCount(), "1 head + 1 member")
	assert.EqualValues(t, 1, filtered.Msg.GetLabs()[0].GetResourceCount())

	// No filter returns both.
	all, err := op.ListLabs(ctx, connect.NewRequest(&devv1.ListLabsRequest{}))
	require.NoError(t, err)
	assert.Len(t, all.Msg.GetLabs(), 2)

	// GetLab returns the full state.
	got, err := op.GetLab(ctx, connect.NewRequest(&devv1.GetLabRequest{LabId: alpha.Msg.GetLab().GetId()}))
	require.NoError(t, err)
	assert.Equal(t, "alpha", got.Msg.GetLab().GetName())
	assert.Len(t, got.Msg.GetMembers(), 2)
	assert.Len(t, got.Msg.GetResources(), 1)
	assert.Len(t, got.Msg.GetPools(), 1)
	assert.Empty(t, got.Msg.GetSlots(), "a fresh workspace has no slots")
}

// TestOperatorTeardown: a torn-down workspace disappears; tearing down a missing one
// is NotFound.
func (s *IntegrationSuite) TestOperatorTeardown() {
	t := s.T()
	ctx := context.Background()
	op := h.operatorClient()

	prov, err := op.ProvisionLab(ctx, connect.NewRequest(&devv1.ProvisionLabRequest{Feature: "temp", MemberCount: 1, ResourceCount: 1}))
	require.NoError(t, err)
	labID := prov.Msg.GetLab().GetId()

	_, err = op.TeardownLab(ctx, connect.NewRequest(&devv1.TeardownLabRequest{LabId: labID}))
	require.NoError(t, err)

	_, err = op.GetLab(ctx, connect.NewRequest(&devv1.GetLabRequest{LabId: labID}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "torn-down lab is gone")

	_, err = op.TeardownLab(ctx, connect.NewRequest(&devv1.TeardownLabRequest{LabId: uuid.NewString()}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "tearing down a missing lab is NotFound")
}

// TestOperatorSecretRequired: the operator surface rejects calls without the correct
// secret — the gate that keeps it from being an open provisioning/impersonation API.
func (s *IntegrationSuite) TestOperatorSecretRequired() {
	t := s.T()
	ctx := context.Background()
	req := func(c interface {
		ListLabs(context.Context, *connect.Request[devv1.ListLabsRequest]) (*connect.Response[devv1.ListLabsResponse], error)
	}) error {
		_, err := c.ListLabs(ctx, connect.NewRequest(&devv1.ListLabsRequest{}))
		return err
	}

	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(req(h.operatorClientWithSecret(""))), "no secret is rejected")
	assert.Equal(t, connect.CodePermissionDenied, connect.CodeOf(req(h.operatorClientWithSecret("wrong"))), "wrong secret is rejected")
	require.NoError(t, req(h.operatorClient()), "the correct secret is accepted")
}
