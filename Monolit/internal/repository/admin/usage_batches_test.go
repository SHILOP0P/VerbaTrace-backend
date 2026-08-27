//go:build integration

package admin

import (
	"github.com/google/uuid"

	"verbatrace/monolit/internal/models"
)

func (s *RepositorySuite) TestAllowanceResetBatchExecutesIdempotentlyWithoutChangingWallet() {
	actor := s.createUser(models.UserRoleSuperAdmin)
	first := s.createUser(models.UserRoleUser)
	second := s.createUser(models.UserRoleUser)
	batch, err := s.repository.CreateAllowanceResetBatch(s.ctx, actor.ID, "user", []uuid.UUID{first.ID, second.ID}, "monthly support reset", "batch-small-1")
	s.Require().NoError(err)
	s.Require().Equal("approved", batch.Status)
	s.Require().Equal(2, batch.Total)
	done, err := s.repository.ExecuteAllowanceResetBatch(s.ctx, batch.ID, actor.ID)
	s.Require().NoError(err)
	s.Require().Equal("completed", done.Status)
	s.Require().Equal(2, done.Succeeded)
	again, err := s.repository.CreateAllowanceResetBatch(s.ctx, actor.ID, "user", []uuid.UUID{first.ID, second.ID}, "monthly support reset", "batch-small-1")
	s.Require().NoError(err)
	s.Require().Equal(batch.ID, again.ID)
	var purchased int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT count(*) FROM credit_grants WHERE grant_type='purchased'`).Scan(&purchased))
	s.Require().Zero(purchased)
}

func (s *RepositorySuite) TestLargeAllowanceResetBatchRequiresDifferentApprover() {
	requester := s.createUser(models.UserRoleSuperAdmin)
	approver := s.createUser(models.UserRoleAdmin)
	owners := make([]uuid.UUID, 0, 101)
	for range 101 {
		owners = append(owners, s.createUser(models.UserRoleUser).ID)
	}
	matched, err := s.repository.PreviewAllowanceResetBatch(s.ctx, requester.ID, "user", owners)
	s.Require().NoError(err)
	s.Require().Equal(101, matched)
	batch, err := s.repository.CreateAllowanceResetBatch(s.ctx, requester.ID, "user", owners, "monthly mass reset", "batch-large-1")
	s.Require().NoError(err)
	s.Require().Equal("draft", batch.Status)
	s.Require().True(batch.RequiresSecondApproval)
	_, err = s.repository.ApproveAllowanceResetBatch(s.ctx, batch.ID, requester.ID)
	s.Require().ErrorIs(err, models.ErrForbidden)
	approved, err := s.repository.ApproveAllowanceResetBatch(s.ctx, batch.ID, approver.ID)
	s.Require().NoError(err)
	s.Require().Equal("approved", approved.Status)
}
