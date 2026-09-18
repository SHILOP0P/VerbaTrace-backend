package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// InvitationCreated writes to a registered user that they were invited, at the
// account's address. The letter goes through the queue and so, for now, only
// to the mock sender. It never fails the invitation.
func (s *Service) InvitationCreated(ctx context.Context, invitationID uuid.UUID) {
	if err := s.invitationLetter(ctx, invitationID); err != nil {
		s.log.Warn(ctx, "invitation letter not queued", zap.String("invitation_id", invitationID.String()), zap.Error(err))
	}
}

func (s *Service) invitationLetter(ctx context.Context, invitationID uuid.UUID) error {
	var (
		user                uuid.UUID
		address, company    string
		department, inviter sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT i.invited_user_uuid, u.email, c.name, d.name,
		       NULLIF(btrim(COALESCE(p.full_name, '') || ' ' || COALESCE(p.full_surname, '')), '')
		FROM membership_invitations i
		JOIN users u ON u.user_uuid = i.invited_user_uuid
		JOIN companies c ON c.company_uuid = i.company_uuid
		LEFT JOIN departments d ON d.department_uuid = i.department_uuid
		LEFT JOIN user_profiles p ON p.user_uuid = i.invited_by_user_uuid
		WHERE i.invitation_uuid = $1`, invitationID).Scan(&user, &address, &company, &department, &inviter)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read invitation: %w", err)
	}
	who := inviter.String
	if who == "" {
		who = "Коллега"
	}
	var text strings.Builder
	fmt.Fprintf(&text, "%s приглашает вас в компанию %s", who, company)
	if department.Valid && department.String != "" {
		fmt.Fprintf(&text, ", отдел %s", department.String)
	}
	fmt.Fprintf(&text, ". Принять или отклонить приглашение: %s", s.Link("/invitations"))
	_, err = Enqueue(ctx, s.db, Outbound{User: user, Channel: ChannelEmail, Kind: "invitation", DedupeKey: "invitation:" + invitationID.String(),
		Payload: map[string]any{"address": address, "title": fmt.Sprintf("Вас пригласили в компанию %s в VerbaTrace", company), "text": text.String()}})
	return err
}
