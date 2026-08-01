//go:build integration

package admin

import (
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (s *RepositorySuite) TestListAndGetAdminCompanies() {
	manager := s.createUser(models.UserRoleUser)
	companyID := uuid.New()
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	_, err := s.db.ExecContext(s.ctx, `
		INSERT INTO companies (company_uuid, name, tag, manager_user_uuid, member_limit, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, companyID, "VerbaTrace", "@"+companyID.String(), manager.ID, 10, createdAt)
	s.Require().NoError(err)

	listed, err := s.repository.ListAdminCompanies(s.ctx, models.ListAdminCompaniesInput{Query: "verb", Limit: 50})
	s.Require().NoError(err)
	s.Require().Equal(1, listed.Total)
	s.Require().Len(listed.Companies, 1)
	s.Require().Equal(companyID, listed.Companies[0].ID)
	s.Require().Equal("@"+companyID.String(), listed.Companies[0].Tag)
	s.Require().Equal(manager.ID, listed.Companies[0].ManagerUserUUID)

	company, err := s.repository.GetAdminCompanyByUUID(s.ctx, companyID)
	s.Require().NoError(err)
	s.Require().Equal(companyID, company.ID)
	s.Require().Equal("@"+companyID.String(), company.Tag)
	s.Require().Equal(manager.ID, company.ManagerUserUUID)
}

func (s *RepositorySuite) TestGrantExtendAndCancelPersonalSubscription() {
	actor := s.createUser(models.UserRoleAdmin)
	target := s.createUser(models.UserRoleUser)
	now := time.Now().UTC().Truncate(time.Second)
	reason := "manual payment"
	granted, err := s.repository.GrantAdminSubscription(s.ctx, models.GrantAdminSubscriptionInput{
		ActorUserUUID: actor.ID, UserUUID: target.ID, PlanCode: models.PlanCodePersonalPlus,
		StartsAt: now, EndsAt: now.Add(30 * 24 * time.Hour), Metadata: models.AdminMutationMetadata{Reason: reason},
	})
	s.Require().NoError(err)
	s.Require().Equal(models.SubscriptionStatusActive, granted.Status)
	s.Require().Equal(models.PlanCodePersonalPlus, granted.PlanCode)

	extendedEnd := now.Add(60 * 24 * time.Hour)
	extended, err := s.repository.GrantAdminSubscription(s.ctx, models.GrantAdminSubscriptionInput{
		ActorUserUUID: actor.ID, UserUUID: target.ID, PlanCode: models.PlanCodePersonalPlus,
		StartsAt: now, EndsAt: extendedEnd, Metadata: models.AdminMutationMetadata{Reason: reason},
	})
	s.Require().NoError(err)
	s.Require().Equal(granted.ID, extended.ID)
	s.Require().NotNil(extended.EndsAt)
	s.Require().True(extended.EndsAt.Equal(extendedEnd))

	canceled, err := s.repository.CancelAdminSubscription(s.ctx, models.CancelAdminSubscriptionInput{ActorUserUUID: actor.ID, UserUUID: target.ID, Metadata: models.AdminMutationMetadata{Reason: reason}})
	s.Require().NoError(err)
	s.Require().Equal(models.SubscriptionStatusCanceled, canceled.Status)
	var count int
	s.Require().NoError(s.db.QueryRowContext(s.ctx, `SELECT COUNT(*) FROM admin_audit_logs WHERE action IN ('subscription.granted','subscription.extended','subscription.canceled')`).Scan(&count))
	s.Require().Equal(3, count)
}
