package supportaccess

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
)

// AuthorizedSubjects returns only subjects covered by an active, exact-resource grant.
// It is intended for collection endpoints; each grant use is recorded once per request.
func (s *Service) AuthorizedSubjects(ctx context.Context, actor uuid.UUID, resource string) ([]uuid.UUID, []uuid.UUID, error) {
	if actor == uuid.Nil || !onlyAllowed([]string{resource}, allowedResources) {
		return nil, nil, ErrForbidden
	}
	rows, err := s.db.QueryContext(ctx, `SELECT grant_uuid,subject_type,subject_user_uuid,subject_company_uuid
		FROM support_access_grants WHERE grantee_user_uuid=$1 AND $2=ANY(resource_allowlist)
		AND valid_from<=now() AND expires_at>now() AND revoked_at IS NULL ORDER BY expires_at`, actor, resource)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()
	users, companies, grants := []uuid.UUID{}, []uuid.UUID{}, []uuid.UUID{}
	seenUsers, seenCompanies := map[uuid.UUID]struct{}{}, map[uuid.UUID]struct{}{}
	for rows.Next() {
		var grant uuid.UUID
		var kind string
		var userID, companyID uuid.NullUUID
		if err = rows.Scan(&grant, &kind, &userID, &companyID); err != nil {
			return nil, nil, err
		}
		grants = append(grants, grant)
		if kind == "user" && userID.Valid {
			appendUniqueUUID(&users, seenUsers, userID.UUID)
		}
		if kind == "company" && companyID.Valid {
			appendUniqueUUID(&companies, seenCompanies, companyID.UUID)
			members, memberErr := s.db.QueryContext(ctx, `SELECT user_uuid FROM company_members WHERE company_uuid=$1 AND status='active'`, companyID.UUID)
			if memberErr != nil {
				return nil, nil, memberErr
			}
			for members.Next() {
				var member uuid.UUID
				if memberErr = members.Scan(&member); memberErr != nil {
					_ = members.Close()
					return nil, nil, memberErr
				}
				appendUniqueUUID(&users, seenUsers, member)
			}
			memberErr = members.Err()
			_ = members.Close()
			if memberErr != nil {
				return nil, nil, memberErr
			}
		}
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	for _, grant := range grants {
		if _, err = s.db.ExecContext(ctx, `INSERT INTO support_access_events(event_uuid,grant_uuid,actor_user_uuid,event_type,resource) VALUES($1,$2,$3,'resource_accessed',$4)`, uuid.New(), grant, actor, resource); err != nil {
			return nil, nil, err
		}
	}
	return users, companies, nil
}

func (s *Service) AuthorizeUser(ctx context.Context, actor, userID uuid.UUID, resource, command string) error {
	return s.authorizeResolved(ctx, actor, resource, command, `
		(g.subject_type='user' AND g.subject_user_uuid=$3) OR
		(g.subject_type='company' AND EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=g.subject_company_uuid AND cm.user_uuid=$3 AND cm.status='active'))`, userID)
}

func (s *Service) AuthorizeCompany(ctx context.Context, actor, companyID uuid.UUID, resource, command string) error {
	return s.authorizeResolved(ctx, actor, resource, command, `g.subject_type='company' AND g.subject_company_uuid=$3`, companyID)
}

func (s *Service) AuthorizeCall(ctx context.Context, actor, callID uuid.UUID, resource, command string) error {
	var companyID, ownerID uuid.NullUUID
	err := s.db.QueryRowContext(ctx, `SELECT company_uuid,uploaded_by_user_uuid FROM calls WHERE call_uuid=$1`, callID).Scan(&companyID, &ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if companyID.Valid {
		return s.AuthorizeCompany(ctx, actor, companyID.UUID, resource, command)
	}
	if ownerID.Valid {
		return s.AuthorizeUser(ctx, actor, ownerID.UUID, resource, command)
	}
	return ErrForbidden
}

func (s *Service) AuthorizeAction(ctx context.Context, actor, actionID uuid.UUID, resource, command string) error {
	var companyID uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT company_uuid FROM call_actions WHERE action_uuid=$1`, actionID).Scan(&companyID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	return s.AuthorizeCompany(ctx, actor, companyID, resource, command)
}

func (s *Service) authorizeResolved(ctx context.Context, actor uuid.UUID, resource, command, subjectPredicate string, subjectID uuid.UUID) error {
	if actor == uuid.Nil || subjectID == uuid.Nil || !onlyAllowed([]string{resource}, allowedResources) || command != "" && !onlyAllowed([]string{command}, allowedCommands) {
		return ErrForbidden
	}
	query := `SELECT g.grant_uuid FROM support_access_grants g WHERE g.grantee_user_uuid=$1 AND $2=ANY(g.resource_allowlist) AND (` + subjectPredicate + `)
		AND ($4='' OR $4=ANY(g.command_allowlist)) AND g.valid_from<=now() AND g.expires_at>now() AND g.revoked_at IS NULL ORDER BY g.expires_at LIMIT 1`
	var grant uuid.UUID
	if err := s.db.QueryRowContext(ctx, query, actor, resource, subjectID, command).Scan(&grant); errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	} else if err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO support_access_events(event_uuid,grant_uuid,actor_user_uuid,event_type,resource,command) VALUES($1,$2,$3,'resource_accessed',$4,NULLIF($5,''))`, uuid.New(), grant, actor, resource, command)
	return err
}

func appendUniqueUUID(items *[]uuid.UUID, seen map[uuid.UUID]struct{}, id uuid.UUID) {
	if id == uuid.Nil {
		return
	}
	if _, exists := seen[id]; exists {
		return
	}
	seen[id] = struct{}{}
	*items = append(*items, id)
}
