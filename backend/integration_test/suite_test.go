//go:build integration

package integrationtest

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// IntegrationSuite groups the full-stack cases. It shares the one running server
// and database (set up in TestMain); SetupTest resets DB state and the clock before
// each test, so cases stay independent without repeating the reset by hand.
//
// Cases run serially (the shared DB + clock are not safe for t.Parallel). Multi-
// step chains fall out naturally: within a method, state persists across calls.
type IntegrationSuite struct {
	suite.Suite
}

// TestIntegration is the single Go test entrypoint; testify runs each Test* method.
func TestIntegration(t *testing.T) {
	suite.Run(t, new(IntegrationSuite))
}

// SetupTest returns the database to a clean slate and the clock to base before each
// case (TRUNCATE ... RESTART IDENTITY CASCADE via the admin pool).
func (s *IntegrationSuite) SetupTest() {
	h.reset(s.T())
}
