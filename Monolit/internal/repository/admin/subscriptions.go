package admin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"verbatrace/monolit/internal/models"

	"github.com/google/uuid"
)

func (r *Repository) ListAdminCompanies(ctx context.Context, input models.ListAdminCompaniesInput) (models.ListAdminCompaniesResult, error) {
	args := []any{}
	where := "deleted_at IS NULL"
	if input.VisibleCompanyUUIDs != nil {
		args = append(args, input.VisibleCompanyUUIDs)
		where += fmt.Sprintf(" AND company_uuid=ANY($%d)", len(args))
	}
	if q := strings.TrimSpace(input.Query); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		where += fmt.Sprintf(" AND LOWER(name) LIKE $%d", len(args))
	}
	args = append(args, input.Limit, input.Offset)
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf("SELECT company_uuid,name,tag,manager_user_uuid,created_at,COUNT(*) OVER() FROM companies WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d", where, len(args)-1, len(args)), args...)
	if err != nil {
		return models.ListAdminCompaniesResult{}, err
	}
	defer func() { _ = rows.Close() }()
	res := models.ListAdminCompaniesResult{Companies: []models.AdminCompany{}, Limit: input.Limit, Offset: input.Offset}
	for rows.Next() {
		var c models.AdminCompany
		var total int
		if err := rows.Scan(&c.ID, &c.Name, &c.Tag, &c.ManagerUserUUID, &c.CreatedAt, &total); err != nil {
			return res, err
		}
		res.Companies = append(res.Companies, c)
		res.Total = total
	}
	return res, rows.Err()
}
func (r *Repository) GetAdminCompanyByUUID(ctx context.Context, id uuid.UUID) (models.AdminCompany, error) {
	var c models.AdminCompany
	err := r.db.QueryRowContext(ctx, "SELECT company_uuid,name,tag,manager_user_uuid,created_at FROM companies WHERE company_uuid=$1 AND deleted_at IS NULL", id).Scan(&c.ID, &c.Name, &c.Tag, &c.ManagerUserUUID, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, models.ErrCompanyNotFound
	}
	return c, err
}
func getAdminCompany(ctx context.Context, q queryRower, id uuid.UUID) (models.AdminCompany, error) {
	var c models.AdminCompany
	err := q.QueryRowContext(ctx, "SELECT company_uuid,name,tag,manager_user_uuid,created_at FROM companies WHERE company_uuid=$1 AND deleted_at IS NULL", id).Scan(&c.ID, &c.Name, &c.Tag, &c.ManagerUserUUID, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, models.ErrCompanyNotFound
	}
	return c, err
}
func (r *Repository) GetAdminPersonalSubscription(ctx context.Context, id uuid.UUID) (models.AdminSubscription, error) {
	return getAdminSubscription(ctx, r.db, models.PlanTypePersonal, id)
}

// GetAdminCompanySubscription answers with the plan that covers this company.
// A business subscription belongs to the owner of the company, not to the
// company itself, so the lookup goes through the owner.
func (r *Repository) GetAdminCompanySubscription(ctx context.Context, id uuid.UUID) (models.AdminSubscription, error) {
	company, err := getAdminCompany(ctx, r.db, id)
	if err != nil {
		return models.AdminSubscription{}, err
	}
	return getAdminSubscription(ctx, r.db, models.PlanTypeBusiness, company.ManagerUserUUID)
}

// getAdminSubscription always looks a subscription up by its owning user. The
// type has to be part of the condition: one person may hold a personal and a
// business plan at the same time, and without it the newer one would win.
func getAdminSubscription(ctx context.Context, q queryRower, subscriptionType models.PlanType, ownerUser uuid.UUID) (models.AdminSubscription, error) {
	row := q.QueryRowContext(ctx, `SELECT s.subscription_uuid,p.code,s.type,s.status,s.user_uuid,s.company_uuid,s.starts_at,s.ends_at,s.created_at,s.updated_at FROM subscriptions s JOIN plans p ON p.plan_uuid=s.plan_uuid WHERE s.user_uuid=$1 AND s.type=$2 AND s.status='active' AND s.starts_at<=now() AND (s.ends_at IS NULL OR s.ends_at>now()) ORDER BY s.starts_at DESC LIMIT 1`, ownerUser, subscriptionType)
	return scanAdminSubscription(row)
}

func getAdminSubscriptionByPlan(ctx context.Context, q queryRower, ownerUser, planID uuid.UUID) (models.AdminSubscription, error) {
	row := q.QueryRowContext(ctx, `SELECT s.subscription_uuid,p.code,s.type,s.status,s.user_uuid,s.company_uuid,s.starts_at,s.ends_at,s.created_at,s.updated_at FROM subscriptions s JOIN plans p ON p.plan_uuid=s.plan_uuid WHERE s.user_uuid=$1 AND s.plan_uuid=$2 AND s.status='active' AND (s.ends_at IS NULL OR s.ends_at>now()) ORDER BY s.starts_at DESC,s.created_at DESC,s.subscription_uuid DESC LIMIT 1`, ownerUser, planID)
	return scanAdminSubscription(row)
}

func scanAdminSubscription(row interface{ Scan(...any) error }) (models.AdminSubscription, error) {
	var sub models.AdminSubscription
	var code, typ, status string
	var end sql.NullTime
	err := row.Scan(&sub.ID, &code, &typ, &status, &sub.UserUUID, &sub.CompanyUUID, &sub.StartsAt, &end, &sub.CreatedAt, &sub.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return sub, models.ErrSubscriptionNotFound
	}
	if err != nil {
		return sub, err
	}
	sub.PlanCode = models.PlanCode(code)
	sub.Type = models.PlanType(typ)
	sub.Status = models.SubscriptionStatus(status)
	if end.Valid {
		sub.EndsAt = &end.Time
	}
	return sub, nil
}
func (r *Repository) GrantAdminSubscription(ctx context.Context, in models.GrantAdminSubscriptionInput) (models.AdminSubscription, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AdminSubscription{}, err
	}
	defer func() { _ = tx.Rollback() }()
	actor, err := getAdminUserForUpdate(ctx, tx, in.ActorUserUUID)
	if err != nil {
		return models.AdminSubscription{}, err
	}
	if actor.Role != models.UserRoleAdmin && actor.Role != models.UserRoleSuperAdmin {
		return models.AdminSubscription{}, models.ErrForbidden
	}
	// The plan is always stored against a user. For a business plan that user is
	// the owner of the company, while the audit trail still points at the company
	// the administrator acted on.
	ownerUser, subscriptionType := in.UserUUID, models.PlanTypePersonal
	targetType, targetID := "user", in.UserUUID
	if in.CompanyUUID != uuid.Nil {
		subscriptionType, targetType, targetID = models.PlanTypeBusiness, "company", in.CompanyUUID
		company, companyErr := getAdminCompany(ctx, tx, in.CompanyUUID)
		if companyErr != nil {
			return models.AdminSubscription{}, companyErr
		}
		ownerUser = company.ManagerUserUUID
	} else if _, err := getAdminUser(ctx, tx, ownerUser); err != nil {
		return models.AdminSubscription{}, err
	}
	var planID uuid.UUID
	var planType string
	if err := tx.QueryRowContext(ctx, "SELECT plan_uuid,type FROM plans WHERE code=$1", in.PlanCode).Scan(&planID, &planType); errors.Is(err, sql.ErrNoRows) {
		return models.AdminSubscription{}, models.ErrPlanNotFound
	} else if err != nil {
		return models.AdminSubscription{}, err
	}
	if models.PlanType(planType) != subscriptionType {
		return models.AdminSubscription{}, models.ErrInvalidBillingInput
	}
	old, oldErr := getAdminSubscriptionByPlan(ctx, tx, ownerUser, planID)
	if oldErr == nil {
		if _, err = tx.ExecContext(ctx, "UPDATE subscriptions SET ends_at=$2,updated_at=now() WHERE subscription_uuid=$1", old.ID, in.EndsAt); err != nil {
			return models.AdminSubscription{}, err
		}
		old.EndsAt = &in.EndsAt
		old.UpdatedAt = time.Now().UTC()
		after, _ := json.Marshal(map[string]string{"plan_code": string(in.PlanCode), "status": "active", "operation": "extended"})
		if err = insertAudit(ctx, tx, auditForSubscription(actor, targetType, targetID, "subscription.extended", after, in.Metadata)); err != nil {
			return models.AdminSubscription{}, err
		}
		return old, tx.Commit()
	}
	if oldErr != nil && !errors.Is(oldErr, models.ErrSubscriptionNotFound) {
		return models.AdminSubscription{}, oldErr
	}
	// Only a plan of the same kind is replaced: granting a business plan must not
	// cancel the same person's personal one.
	if _, err = tx.ExecContext(ctx, "UPDATE subscriptions SET status='canceled',ends_at=GREATEST(starts_at + INTERVAL '1 second',$3),updated_at=now() WHERE user_uuid=$1 AND type=$2 AND status='active'", ownerUser, subscriptionType, in.StartsAt); err != nil {
		return models.AdminSubscription{}, err
	}
	id := mustUUIDv7()
	if _, err = tx.ExecContext(ctx, `INSERT INTO subscriptions(subscription_uuid,plan_uuid,type,user_uuid,company_uuid,status,starts_at,ends_at) VALUES($1,$2,$3,$4,NULL,'active',$5,$6)`, id, planID, subscriptionType, ownerUser, in.StartsAt, in.EndsAt); err != nil {
		return models.AdminSubscription{}, err
	}
	after, _ := json.Marshal(map[string]string{"plan_code": string(in.PlanCode), "status": "active"})
	if err = insertAudit(ctx, tx, auditForSubscription(actor, targetType, targetID, "subscription.granted", after, in.Metadata)); err != nil {
		return models.AdminSubscription{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.AdminSubscription{}, err
	}
	now := time.Now().UTC()
	return models.AdminSubscription{ID: id, PlanCode: in.PlanCode, Type: subscriptionType, Status: models.SubscriptionStatusActive, UserUUID: uuid.NullUUID{UUID: ownerUser, Valid: true}, StartsAt: in.StartsAt, EndsAt: &in.EndsAt, CreatedAt: now, UpdatedAt: now}, nil
}
func (r *Repository) CancelAdminSubscription(ctx context.Context, in models.CancelAdminSubscriptionInput) (models.AdminSubscription, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AdminSubscription{}, err
	}
	defer func() { _ = tx.Rollback() }()
	actor, err := getAdminUserForUpdate(ctx, tx, in.ActorUserUUID)
	if err != nil {
		return models.AdminSubscription{}, err
	}
	if actor.Role != models.UserRoleAdmin && actor.Role != models.UserRoleSuperAdmin {
		return models.AdminSubscription{}, models.ErrForbidden
	}
	subscriptionType, ownerUser := models.PlanTypePersonal, in.UserUUID
	targetType, targetID := "user", in.UserUUID
	if in.CompanyUUID != uuid.Nil {
		subscriptionType, targetType, targetID = models.PlanTypeBusiness, "company", in.CompanyUUID
		company, companyErr := getAdminCompany(ctx, tx, in.CompanyUUID)
		if companyErr != nil {
			return models.AdminSubscription{}, companyErr
		}
		ownerUser = company.ManagerUserUUID
	}
	sub, err := getAdminSubscription(ctx, tx, subscriptionType, ownerUser)
	if err != nil {
		return models.AdminSubscription{}, err
	}
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, "UPDATE subscriptions SET status='canceled',ends_at=$2,updated_at=now() WHERE subscription_uuid=$1", sub.ID, now); err != nil {
		return models.AdminSubscription{}, err
	}
	after, _ := json.Marshal(map[string]string{"status": "canceled"})
	if err = insertAudit(ctx, tx, models.AdminAuditLog{ID: mustUUIDv7(), ActorUserUUID: actor.ID, ActorRole: actor.Role, Action: "subscription.canceled", TargetType: targetType, TargetUUID: uuid.NullUUID{UUID: targetID, Valid: true}, AfterData: after, Reason: &in.Metadata.Reason, RequestID: in.Metadata.RequestID, IPAddress: in.Metadata.IPAddress, UserAgent: in.Metadata.UserAgent, CreatedAt: now}); err != nil {
		return models.AdminSubscription{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.AdminSubscription{}, err
	}
	sub.Status = models.SubscriptionStatusCanceled
	sub.EndsAt = &now
	return sub, nil
}
func auditForSubscription(actor models.AdminUser, targetType string, targetID uuid.UUID, action string, after json.RawMessage, metadata models.AdminMutationMetadata) models.AdminAuditLog {
	return models.AdminAuditLog{ID: mustUUIDv7(), ActorUserUUID: actor.ID, ActorRole: actor.Role, Action: action, TargetType: targetType, TargetUUID: uuid.NullUUID{UUID: targetID, Valid: true}, AfterData: after, Reason: &metadata.Reason, RequestID: metadata.RequestID, IPAddress: metadata.IPAddress, UserAgent: metadata.UserAgent, CreatedAt: time.Now().UTC()}
}
