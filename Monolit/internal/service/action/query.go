package action

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// statusRevertWindow is how long a status change can be taken back.
const statusRevertWindow = time.Hour

const actionSelect = `SELECT a.action_uuid,a.company_uuid,COALESCE(c.name,''),COALESCE(c.tag,''),CASE WHEN a.company_uuid IS NULL THEN 'personal' ELSE 'company' END,CASE WHEN a.company_uuid IS NULL THEN p.username ELSE COALESCE(NULLIF(c.tag,''),a.company_uuid::text) END,a.source_department_uuid,COALESCE(sd.name,''),a.target_department_uuid,COALESCE(td.name,''),a.call_uuid,a.analysis_uuid,a.transcription_revision,a.title,a.description,a.status,a.assignment_state,a.assignee_user_uuid,p.username,a.due_at,a.grace_expires_at,a.lock_version,a.created_by_user_uuid,a.created_at,a.updated_at,a.completed_at,a.cancelled_at,a.cancel_reason FROM call_actions a JOIN user_profiles p ON p.user_uuid=a.assignee_user_uuid LEFT JOIN companies c ON c.company_uuid=a.company_uuid LEFT JOIN departments sd ON sd.department_uuid=a.source_department_uuid LEFT JOIN departments td ON td.department_uuid=a.target_department_uuid`

func (s *Service) Get(ctx context.Context, id, actor uuid.UUID, admin bool) (Item, error) {
	row := s.db.QueryRowContext(ctx, actionSelect+` WHERE a.action_uuid=$1`, id)
	item, err := scanItem(row)
	if err == sql.ErrNoRows {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	caps, visible, err := s.access(ctx, item, actor, admin)
	if err != nil {
		return Item{}, err
	}
	if !visible {
		return Item{}, ErrNotFound
	}
	item.Capabilities = caps
	item.Evidence, err = s.evidence(ctx, id)
	return item, err
}

type scanner interface{ Scan(...any) error }

func scanItem(row scanner) (Item, error) {
	var x Item
	var completed, cancelled sql.NullTime
	var reason sql.NullString
	var company, source, target uuid.NullUUID
	err := row.Scan(&x.ID, &company, &x.CompanyName, &x.CompanyTag, &x.ScopeType, &x.ScopeTag, &source, &x.SourceDepartmentName, &target, &x.TargetDepartmentName, &x.CallUUID, &x.AnalysisUUID, &x.TranscriptionRevision, &x.Title, &x.Description, &x.Status, &x.AssignmentState, &x.AssigneeUserUUID, &x.AssigneeUsername, &x.DueAt, &x.GraceExpiresAt, &x.LockVersion, &x.CreatedByUserUUID, &x.CreatedAt, &x.UpdatedAt, &completed, &cancelled, &reason)
	x.CompanyUUID, x.SourceDepartmentUUID, x.TargetDepartmentUUID = nullableUUID(company), nullableUUID(source), nullableUUID(target)
	x.CompletedAt = ptrTime(completed)
	x.CancelledAt = ptrTime(cancelled)
	x.CancelReason = ptrString(reason)
	x.Evidence = []Evidence{}
	return x, err
}

func (s *Service) access(ctx context.Context, item Item, actor uuid.UUID, admin bool) (Capabilities, bool, error) {
	if actor == uuid.Nil {
		return Capabilities{}, false, nil
	}
	if item.CompanyUUID == nil {
		adminAllowed := false
		if admin {
			_ = s.db.QueryRowContext(ctx, `SELECT role IN ('admin','superadmin') FROM users WHERE user_uuid=$1`, actor).Scan(&adminAllowed)
		}
		visible := actor == item.AssigneeUserUUID || actor == item.CreatedByUserUUID || adminAllowed
		terminal := item.Status == "completed" || item.Status == "cancelled"
		revertible, err := s.statusRevertible(ctx, item.ID)
		if err != nil {
			return Capabilities{}, false, err
		}
		return Capabilities{
			CanStart:        !terminal && actor == item.AssigneeUserUUID,
			CanComplete:     !terminal && actor == item.AssigneeUserUUID,
			CanReschedule:   !terminal && (actor == item.AssigneeUserUUID || adminAllowed),
			CanEditFields:   !terminal && actor == item.CreatedByUserUUID,
			CanRevertStatus: revertible && (actor == item.AssigneeUserUUID || adminAllowed),
		}, visible, nil
	}
	var manager, leaderSource, leaderTarget bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$2 AND user_uuid=$1 AND status='active' AND role IN ('company_manager','company_deputy')),EXISTS(SELECT 1 FROM department_members WHERE department_uuid=$3 AND user_uuid=$1 AND status='active' AND role='department_leader'),EXISTS(SELECT 1 FROM department_members WHERE department_uuid=$4 AND user_uuid=$1 AND status='active' AND role='department_leader')`, actor, *item.CompanyUUID, item.SourceDepartmentUUID, item.TargetDepartmentUUID).Scan(&manager, &leaderSource, &leaderTarget)
	if err != nil {
		return Capabilities{}, false, err
	}
	adminAllowed := false
	if admin {
		err = s.db.QueryRowContext(ctx, `SELECT role IN ('admin','superadmin') FROM users WHERE user_uuid=$1`, actor).Scan(&adminAllowed)
		if err == sql.ErrNoRows {
			return Capabilities{}, false, nil
		}
		if err != nil {
			return Capabilities{}, false, err
		}
	}
	visible := actor == item.AssigneeUserUUID || actor == item.CreatedByUserUUID || manager || leaderSource || leaderTarget || adminAllowed
	terminal := item.Status == "completed" || item.Status == "cancelled"
	manage := manager || leaderSource || leaderTarget || adminAllowed
	assignedByManager := false
	if leaderSource || leaderTarget {
		err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=$1 AND cm.user_uuid=COALESCE((SELECT e.actor_user_uuid FROM call_action_events e WHERE e.action_uuid=$2 AND e.event_type='reassigned' AND e.actor_user_uuid IS NOT NULL ORDER BY e.created_at DESC,e.event_uuid DESC LIMIT 1),$3) AND cm.status='active' AND cm.role IN ('company_manager','company_deputy'))`, *item.CompanyUUID, item.ID, item.CreatedByUserUUID).Scan(&assignedByManager)
		if err != nil {
			return Capabilities{}, false, err
		}
	}
	canReassign := !terminal && (manager || adminAllowed || ((leaderSource || leaderTarget) && !assignedByManager))
	// Whoever set the action may still hand it to somebody else for a day, or
	// until the assignee picks it up. After that it is a management decision.
	if !terminal && actor == item.CreatedByUserUUID && item.Status == "open" && s.now().UTC().Before(item.CreatedAt.Add(24*time.Hour)) {
		canReassign = true
	}
	revertible, err := s.statusRevertible(ctx, item.ID)
	if err != nil {
		return Capabilities{}, false, err
	}
	return Capabilities{
		CanStart:           !terminal && (actor == item.AssigneeUserUUID || manage),
		CanComplete:        !terminal && (actor == item.AssigneeUserUUID || manage),
		CanCancel:          !terminal && manage,
		CanReschedule:      !terminal && manage,
		CanReassign:        canReassign,
		CanRequestTransfer: !terminal && actor == item.AssigneeUserUUID,
		CanResolveTransfer: !terminal && manage,
		CanEditFields:      !terminal && actor == item.CreatedByUserUUID,
		CanRevertStatus:    revertible && (actor == item.AssigneeUserUUID || manage),
	}, visible, nil
}

// statusRevertible answers whether the last status change is still young enough
// to be taken back. An hour is the window for undoing a wrong click; after that
// the history stands.
func (s *Service) statusRevertible(ctx context.Context, actionID uuid.UUID) (bool, error) {
	var createdAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT created_at FROM call_action_events WHERE action_uuid=$1 AND event_type IN ('started','completed','cancelled') ORDER BY created_at DESC,event_uuid DESC LIMIT 1`, actionID).Scan(&createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return createdAt.Valid && s.now().UTC().Before(createdAt.Time.Add(statusRevertWindow)), nil
}

func (s *Service) evidence(ctx context.Context, id uuid.UUID) ([]Evidence, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT evidence_uuid,kind,position,word_start_index,word_end_index,quote_snapshot,speaker_snapshot,start_seconds,end_seconds FROM call_action_evidence WHERE action_uuid=$1 ORDER BY position`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Evidence{}
	for rows.Next() {
		var e Evidence
		var ws, we sql.NullInt64
		var sp sql.NullString
		var st, en sql.NullFloat64
		if err = rows.Scan(&e.ID, &e.Kind, &e.Position, &ws, &we, &e.Quote, &sp, &st, &en); err != nil {
			return nil, err
		}
		if ws.Valid {
			v := int(ws.Int64)
			e.WordStartIndex = &v
		}
		if we.Valid {
			v := int(we.Int64)
			e.WordEndIndex = &v
		}
		e.Speaker = ptrString(sp)
		if st.Valid {
			v := st.Float64
			e.StartSeconds = &v
		}
		if en.Valid {
			v := en.Float64
			e.EndSeconds = &v
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Service) List(ctx context.Context, in ListInput) (ListResult, error) {
	if in.ActorUserUUID == uuid.Nil {
		return ListResult{}, ErrInvalidInput
	}
	if in.Limit <= 0 {
		in.Limit = 25
	}
	if in.Limit > 100 {
		in.Limit = 100
	}
	if in.Offset < 0 {
		in.Offset = 0
	}
	args := []any{in.ActorUserUUID}
	where := []string{}
	if in.Admin {
		where = append(where, `EXISTS(SELECT 1 FROM users au WHERE au.user_uuid=$1 AND au.role IN ('admin','superadmin'))`)
		if in.SupportCompanyUUIDs != nil {
			args = append(args, in.SupportCompanyUUIDs)
			where = append(where, fmt.Sprintf("a.company_uuid=ANY($%d)", len(args)))
		}
	} else {
		where = append(where, `(a.assignee_user_uuid=$1 OR a.created_by_user_uuid=$1 OR EXISTS(SELECT 1 FROM company_members cm WHERE cm.company_uuid=a.company_uuid AND cm.user_uuid=$1 AND cm.status='active' AND cm.role IN ('company_manager','company_deputy')) OR EXISTS(SELECT 1 FROM department_members dm WHERE dm.user_uuid=$1 AND dm.status='active' AND dm.role='department_leader' AND dm.department_uuid IN (a.source_department_uuid,a.target_department_uuid)))`)
	}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if in.CompanyUUID.Valid {
		add("a.company_uuid=$%d", in.CompanyUUID.UUID)
	}
	if in.CallUUID.Valid {
		add("a.call_uuid=$%d", in.CallUUID.UUID)
	}
	if in.DepartmentUUID.Valid {
		add("$%d IN (a.source_department_uuid,a.target_department_uuid)", in.DepartmentUUID.UUID)
	}
	if in.AssigneeUUID.Valid {
		add("a.assignee_user_uuid=$%d", in.AssigneeUUID.UUID)
	}
	if in.Status != "" {
		add("a.status=$%d", in.Status)
	}
	if in.Query != "" {
		args = append(args, in.Query)
		where = append(where, fmt.Sprintf("(a.title ILIKE '%%'||$%d||'%%' OR p.username ILIKE '%%'||$%d||'%%' OR c.name ILIKE '%%'||$%d||'%%' OR c.tag ILIKE '%%'||$%d||'%%' OR sd.name ILIKE '%%'||$%d||'%%' OR td.name ILIKE '%%'||$%d||'%%')", len(args), len(args), len(args), len(args), len(args), len(args)))
	}
	if in.CompanyTag != "" {
		add("c.tag ILIKE '%%'||$%d||'%%'", in.CompanyTag)
	}
	if in.Department != "" {
		args = append(args, in.Department)
		where = append(where, fmt.Sprintf("(sd.name ILIKE '%%'||$%d||'%%' OR td.name ILIKE '%%'||$%d||'%%')", len(args), len(args)))
	}
	if in.Mine {
		where = append(where, "a.assignee_user_uuid=$1")
	}
	base := ` FROM call_actions a JOIN user_profiles p ON p.user_uuid=a.assignee_user_uuid LEFT JOIN companies c ON c.company_uuid=a.company_uuid LEFT JOIN departments sd ON sd.department_uuid=a.source_department_uuid LEFT JOIN departments td ON td.department_uuid=a.target_department_uuid WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*)`+base, args...).Scan(&total); err != nil {
		return ListResult{}, err
	}
	args = append(args, in.Limit, in.Offset)
	rows, err := s.db.QueryContext(ctx, actionSelect+` WHERE `+strings.Join(where, " AND ")+fmt.Sprintf(" ORDER BY a.updated_at DESC,a.action_uuid LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return ListResult{}, err
	}
	defer func() { _ = rows.Close() }()
	items := []Item{}
	for rows.Next() {
		item, scanErr := scanItem(rows)
		if scanErr != nil {
			return ListResult{}, scanErr
		}
		caps, _, accessErr := s.access(ctx, item, in.ActorUserUUID, in.Admin)
		if accessErr != nil {
			return ListResult{}, accessErr
		}
		item.Capabilities = caps
		items = append(items, item)
	}
	return ListResult{Items: items, Total: total, Limit: in.Limit, Offset: in.Offset}, rows.Err()
}

func (s *Service) ListAssignees(ctx context.Context, actor, company uuid.UUID, query string, department uuid.NullUUID, admin bool) ([]Assignee, error) {
	if actor == uuid.Nil || company == uuid.Nil {
		return nil, ErrInvalidInput
	}
	var allowed bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM company_members WHERE company_uuid=$1 AND user_uuid=$2 AND status='active') OR ($3 AND EXISTS(SELECT 1 FROM users WHERE user_uuid=$2 AND role IN ('admin','superadmin')))`, company, actor, admin).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, ErrForbidden
	}
	args := []any{company, "%" + strings.TrimSpace(query) + "%"}
	filter := ""
	if department.Valid {
		args = append(args, department.UUID)
		filter = ` AND d.department_uuid=$3`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT p.user_uuid,p.username,p.full_name,p.full_surname,cm.job_title,d.department_uuid,d.name FROM company_members cm JOIN user_profiles p ON p.user_uuid=cm.user_uuid JOIN department_members dm ON dm.user_uuid=cm.user_uuid AND dm.status='active' JOIN departments d ON d.department_uuid=dm.department_uuid AND d.company_uuid=cm.company_uuid AND d.deleted_at IS NULL WHERE cm.company_uuid=$1 AND cm.status='active' AND (p.username ILIKE $2 OR p.full_name ILIKE $2 OR p.full_surname ILIKE $2)`+filter+` ORDER BY p.username,d.name LIMIT 100`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	byID := map[uuid.UUID]int{}
	out := []Assignee{}
	for rows.Next() {
		var user Assignee
		var dept AssigneeDepartment
		if err = rows.Scan(&user.UserUUID, &user.Username, &user.FullName, &user.FullSurname, &user.JobTitle, &dept.ID, &dept.Name); err != nil {
			return nil, err
		}
		idx, ok := byID[user.UserUUID]
		if !ok {
			user.Departments = []AssigneeDepartment{}
			out = append(out, user)
			idx = len(out) - 1
			byID[user.UserUUID] = idx
		}
		out[idx].Departments = append(out[idx].Departments, dept)
		if len(out) >= 20 {
			break
		}
	}
	return out, rows.Err()
}
