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
	"github.com/jackc/pgx/v5/pgconn"
)

func (r *Repository) ListAdminUsers(ctx context.Context, input models.ListAdminUsersInput) (models.ListAdminUsersResult, error) {
	where, args := adminUsersWhere(input)
	limitPos := len(args) + 1
	args = append(args, input.Limit)
	offsetPos := len(args) + 1
	args = append(args, input.Offset)
	query := fmt.Sprintf(`
		SELECT u.user_uuid, u.email, p.full_name, p.full_surname, p.username, u.role,
		       p.headline, p.phone, p.timezone, u.created_at, COUNT(*) OVER()
		FROM users u
		JOIN user_profiles p ON p.user_uuid = u.user_uuid
		WHERE %s
		ORDER BY u.created_at DESC, u.user_uuid DESC
		LIMIT $%d OFFSET $%d
	`, where, limitPos, offsetPos)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return models.ListAdminUsersResult{}, fmt.Errorf("list admin users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := models.ListAdminUsersResult{Users: []models.AdminUser{}, Limit: input.Limit, Offset: input.Offset}
	for rows.Next() {
		user, total, err := scanAdminUserWithTotal(rows)
		if err != nil {
			return models.ListAdminUsersResult{}, fmt.Errorf("scan admin user: %w", err)
		}
		result.Users = append(result.Users, user)
		result.Total = total
	}
	if err := rows.Err(); err != nil {
		return models.ListAdminUsersResult{}, fmt.Errorf("iterate admin users: %w", err)
	}
	if len(result.Users) == 0 && input.Offset > 0 {
		if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users u JOIN user_profiles p ON p.user_uuid=u.user_uuid WHERE "+where, args[:len(args)-2]...).Scan(&result.Total); err != nil {
			return models.ListAdminUsersResult{}, fmt.Errorf("count admin users: %w", err)
		}
	}
	return result, nil
}

func adminUsersWhere(input models.ListAdminUsersInput) (string, []any) {
	conditions := []string{"TRUE"}
	args := []any{}
	if q := strings.TrimSpace(input.Query); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		p := len(args)
		conditions = append(conditions, fmt.Sprintf("(LOWER(u.email) LIKE $%d OR LOWER(p.username) LIKE $%d OR LOWER(p.full_name) LIKE $%d OR LOWER(p.full_surname) LIKE $%d)", p, p, p, p))
	}
	if input.Role != nil {
		args = append(args, string(*input.Role))
		conditions = append(conditions, fmt.Sprintf("u.role = $%d", len(args)))
	}
	if input.CreatedFrom != nil {
		args = append(args, *input.CreatedFrom)
		conditions = append(conditions, fmt.Sprintf("u.created_at >= $%d", len(args)))
	}
	if input.CreatedTo != nil {
		args = append(args, *input.CreatedTo)
		conditions = append(conditions, fmt.Sprintf("u.created_at <= $%d", len(args)))
	}
	if input.PlanCode != nil {
		args = append(args, string(*input.PlanCode))
		conditions = append(conditions, fmt.Sprintf("EXISTS (SELECT 1 FROM subscriptions s JOIN plans p ON p.plan_uuid=s.plan_uuid WHERE s.user_uuid=u.user_uuid AND s.status='active' AND s.starts_at <= now() AND (s.ends_at IS NULL OR s.ends_at > now()) AND p.code=$%d)", len(args)))
	}
	if input.SubscriptionStatus != nil {
		switch *input.SubscriptionStatus {
		case models.AdminSubscriptionStatusActive:
			conditions = append(conditions, "EXISTS (SELECT 1 FROM subscriptions s WHERE s.user_uuid=u.user_uuid AND s.status='active' AND s.starts_at <= now() AND (s.ends_at IS NULL OR s.ends_at > now()))")
		case models.AdminSubscriptionStatusNone:
			conditions = append(conditions, "NOT EXISTS (SELECT 1 FROM subscriptions s WHERE s.user_uuid=u.user_uuid AND s.status='active' AND s.starts_at <= now() AND (s.ends_at IS NULL OR s.ends_at > now()))")
		case models.AdminSubscriptionStatusCanceled, models.AdminSubscriptionStatusExpired:
			args = append(args, string(*input.SubscriptionStatus))
			conditions = append(conditions, fmt.Sprintf("EXISTS (SELECT 1 FROM subscriptions s WHERE s.user_uuid=u.user_uuid AND s.status=$%d)", len(args)))
		}
	}
	return strings.Join(conditions, " AND "), args
}

func (r *Repository) GetAdminUserByUUID(ctx context.Context, userID uuid.UUID) (models.AdminUser, error) {
	return getAdminUser(ctx, r.db, userID)
}

func (r *Repository) UpdateAdminUserProfile(ctx context.Context, input models.UpdateAdminUserProfileInput) (models.AdminUser, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AdminUser{}, fmt.Errorf("begin admin profile update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	actor, err := getAdminUserForUpdate(ctx, tx, input.ActorUserUUID)
	if err != nil {
		return models.AdminUser{}, err
	}
	target, err := getAdminUserForUpdate(ctx, tx, input.TargetUserUUID)
	if err != nil {
		return models.AdminUser{}, err
	}
	if err := models.ValidateAdminSessionTarget(actor.Role, target.Role); err != nil {
		return models.AdminUser{}, err
	}
	before, _ := json.Marshal(target)
	row := tx.QueryRowContext(ctx, `
		WITH updated AS (
		UPDATE user_profiles
		SET full_name=COALESCE($2,full_name), full_surname=COALESCE($3,full_surname),
			username=COALESCE($4,username), headline=COALESCE($5,headline), phone=COALESCE($6,phone),
			timezone=COALESCE($7,timezone), updated_at=now()
		WHERE user_uuid=$1
		RETURNING *
		)
		SELECT u.user_uuid,u.email,p.full_name,p.full_surname,p.username,u.role,
		       p.headline,p.phone,p.timezone,u.created_at
		FROM users u JOIN updated p ON p.user_uuid=u.user_uuid`,
		target.ID, input.FullName, input.FullSurname, input.Username, input.Post, input.Phone, input.Timezone)
	updated, err := scanAdminUser(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return models.AdminUser{}, models.ErrUserAlreadyExists
		}
		return models.AdminUser{}, fmt.Errorf("update admin user profile: %w", err)
	}
	after, _ := json.Marshal(updated)
	if err := insertAudit(ctx, tx, models.AdminAuditLog{ID: mustUUIDv7(), ActorUserUUID: actor.ID, ActorRole: actor.Role, Action: "user.profile_updated", TargetType: "user", TargetUUID: uuid.NullUUID{UUID: target.ID, Valid: true}, BeforeData: before, AfterData: after, Reason: &input.Metadata.Reason, RequestID: input.Metadata.RequestID, IPAddress: input.Metadata.IPAddress, UserAgent: input.Metadata.UserAgent, CreatedAt: time.Now().UTC()}); err != nil {
		return models.AdminUser{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.AdminUser{}, fmt.Errorf("commit admin profile update: %w", err)
	}
	return updated, nil
}

func getAdminUser(ctx context.Context, q queryRower, userID uuid.UUID) (models.AdminUser, error) {
	row := q.QueryRowContext(ctx, `SELECT u.user_uuid,u.email,p.full_name,p.full_surname,p.username,u.role,p.headline,p.phone,p.timezone,u.created_at FROM users u JOIN user_profiles p ON p.user_uuid=u.user_uuid WHERE u.user_uuid=$1`, userID)
	user, err := scanAdminUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AdminUser{}, models.ErrUserNotFound
	}
	if err != nil {
		return models.AdminUser{}, err
	}
	return user, nil
}

func (r *Repository) ChangeAdminUserRole(ctx context.Context, input models.ChangeAdminUserRoleInput) (models.AdminUser, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.AdminUser{}, fmt.Errorf("begin role change: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	actor, err := getAdminUserForUpdate(ctx, tx, input.ActorUserUUID)
	if err != nil {
		return models.AdminUser{}, err
	}
	target, err := getAdminUserForUpdate(ctx, tx, input.TargetUserUUID)
	if err != nil {
		return models.AdminUser{}, err
	}
	if actor.ID == target.ID {
		return models.AdminUser{}, models.ErrCannotChangeOwnRole
	}
	if target.Role != input.ExpectedRole {
		return models.AdminUser{}, models.ErrUserRoleChanged
	}
	if err := models.ValidateAdminRoleTransition(actor.Role, target.Role, input.Role); err != nil {
		return models.AdminUser{}, err
	}
	before, _ := json.Marshal(map[string]string{"role": string(target.Role)})
	after, _ := json.Marshal(map[string]string{"role": string(input.Role)})
	if _, err = tx.ExecContext(ctx, "UPDATE users SET role=$2 WHERE user_uuid=$1", target.ID, input.Role); err != nil {
		return models.AdminUser{}, fmt.Errorf("update admin user role: %w", err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE refresh_sessions SET access_version=access_version+1 WHERE user_uuid=$1 AND revoked_at IS NULL AND expires_at>now()", target.ID); err != nil {
		return models.AdminUser{}, fmt.Errorf("invalidate target access: %w", err)
	}
	if err = insertAudit(ctx, tx, models.AdminAuditLog{ID: mustUUIDv7(), ActorUserUUID: actor.ID, ActorRole: actor.Role, Action: "user.role_changed", TargetType: "user", TargetUUID: uuid.NullUUID{UUID: target.ID, Valid: true}, BeforeData: before, AfterData: after, Reason: &input.Metadata.Reason, RequestID: input.Metadata.RequestID, IPAddress: input.Metadata.IPAddress, UserAgent: input.Metadata.UserAgent, CreatedAt: time.Now().UTC()}); err != nil {
		return models.AdminUser{}, err
	}
	if err = tx.Commit(); err != nil {
		return models.AdminUser{}, fmt.Errorf("commit role change: %w", err)
	}
	target.Role = input.Role
	return target, nil
}

func (r *Repository) ListAdminUserSessions(ctx context.Context, userID uuid.UUID) ([]models.AdminUserSession, error) {
	if _, err := r.GetAdminUserByUUID(ctx, userID); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT session_uuid,user_agent,ip_address::TEXT,created_at,last_used_at,expires_at FROM refresh_sessions WHERE user_uuid=$1 AND revoked_at IS NULL AND expires_at>now() ORDER BY COALESCE(last_used_at,created_at) DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list admin sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := []models.AdminUserSession{}
	for rows.Next() {
		var s models.AdminUserSession
		var agent, ip sql.NullString
		var last sql.NullTime
		if err := rows.Scan(&s.ID, &agent, &ip, &s.CreatedAt, &last, &s.ExpiresAt); err != nil {
			return nil, err
		}
		if agent.Valid {
			s.UserAgent = &agent.String
		}
		if ip.Valid {
			s.IPAddress = &ip.String
		}
		if last.Valid {
			s.LastSeenAt = &last.Time
		}
		items = append(items, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *Repository) RevokeAdminUserSession(ctx context.Context, input models.AdminSessionMutationInput) error {
	return r.revokeAdminSessions(ctx, input, false)
}
func (r *Repository) RevokeAllAdminUserSessions(ctx context.Context, input models.AdminSessionMutationInput) error {
	return r.revokeAdminSessions(ctx, input, true)
}
func (r *Repository) revokeAdminSessions(ctx context.Context, input models.AdminSessionMutationInput, all bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	actor, err := getAdminUserForUpdate(ctx, tx, input.ActorUserUUID)
	if err != nil {
		return err
	}
	target, err := getAdminUserForUpdate(ctx, tx, input.TargetUserUUID)
	if err != nil {
		return err
	}
	if actor.ID == target.ID {
		return models.ErrAdminSessionManagementForbidden
	}
	if err = models.ValidateAdminSessionTarget(actor.Role, target.Role); err != nil {
		return err
	}
	var result sql.Result
	if all {
		result, err = tx.ExecContext(ctx, "UPDATE refresh_sessions SET revoked_at=COALESCE(revoked_at,now()),revoked_reason=COALESCE(revoked_reason,'admin_revoked') WHERE user_uuid=$1 AND revoked_at IS NULL", target.ID)
	} else {
		result, err = tx.ExecContext(ctx, "UPDATE refresh_sessions SET revoked_at=COALESCE(revoked_at,now()),revoked_reason=COALESCE(revoked_reason,'admin_revoked') WHERE user_uuid=$1 AND session_uuid=$2 AND revoked_at IS NULL", target.ID, input.SessionUUID)
	}
	if err != nil {
		return fmt.Errorf("revoke admin session: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if !all && rows == 0 {
		return models.ErrRefreshSessionNotFound
	}
	after, _ := json.Marshal(map[string]any{"all_sessions": all, "revoked_sessions": rows})
	action := "session.revoked"
	targetID := input.SessionUUID
	if all {
		action = "session.revoked_all"
		targetID = target.ID
	}
	if err = insertAudit(ctx, tx, models.AdminAuditLog{ID: mustUUIDv7(), ActorUserUUID: actor.ID, ActorRole: actor.Role, Action: action, TargetType: "refresh_session", TargetUUID: uuid.NullUUID{UUID: targetID, Valid: true}, AfterData: after, Reason: &input.Metadata.Reason, RequestID: input.Metadata.RequestID, IPAddress: input.Metadata.IPAddress, UserAgent: input.Metadata.UserAgent, CreatedAt: time.Now().UTC()}); err != nil {
		return err
	}
	return tx.Commit()
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getAdminUserForUpdate(ctx context.Context, tx *sql.Tx, userID uuid.UUID) (models.AdminUser, error) {
	row := tx.QueryRowContext(ctx, `SELECT u.user_uuid,u.email,p.full_name,p.full_surname,p.username,u.role,p.headline,p.phone,p.timezone,u.created_at FROM users u JOIN user_profiles p ON p.user_uuid=u.user_uuid WHERE u.user_uuid=$1 FOR UPDATE`, userID)
	u, err := scanAdminUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return models.AdminUser{}, models.ErrUserNotFound
	}
	return u, err
}
func scanAdminUser(row interface{ Scan(...any) error }) (models.AdminUser, error) {
	var u models.AdminUser
	var role string
	var post, phone, tz sql.NullString
	err := row.Scan(&u.ID, &u.Email, &u.FullName, &u.FullSurname, &u.Username, &role, &post, &phone, &tz, &u.CreatedAt)
	if err != nil {
		return u, err
	}
	u.Role = models.UserRole(role)
	if post.Valid {
		u.Post = &post.String
	}
	if phone.Valid {
		u.Phone = &phone.String
	}
	if tz.Valid {
		u.Timezone = &tz.String
	}
	return u, nil
}
func scanAdminUserWithTotal(row interface{ Scan(...any) error }) (models.AdminUser, int, error) {
	var u models.AdminUser
	var total int
	var role string
	var post, phone, tz sql.NullString
	err := row.Scan(&u.ID, &u.Email, &u.FullName, &u.FullSurname, &u.Username, &role, &post, &phone, &tz, &u.CreatedAt, &total)
	if err != nil {
		return u, 0, err
	}
	u.Role = models.UserRole(role)
	if post.Valid {
		u.Post = &post.String
	}
	if phone.Valid {
		u.Phone = &phone.String
	}
	if tz.Valid {
		u.Timezone = &tz.String
	}
	return u, total, nil
}
func mustUUIDv7() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return id
}
