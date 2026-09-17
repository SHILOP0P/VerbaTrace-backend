package bitrix24

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// frozenTaskTitle is what the portal shows. It has to be understandable by
// somebody who has never seen VerbaTrace's own interface.
const frozenTaskTitle = "VerbaTrace: обработка звонков приостановлена"

// NotifyCompanyFrozen tells the portals of a frozen company why their calls
// stopped arriving.
//
// The inbound event hook is one-way — the portal only ever sees an HTTP status,
// which nothing displays — so silence would look like a broken integration. A
// task is the one channel that works with the permissions every connection
// already has, and it is created once, at the moment of the freeze, rather than
// for every event that keeps arriving afterwards.
//
// Nothing here may fail the freeze: the company is already frozen by the time
// this runs, and an unreachable portal is not a reason to undo that.
func (s *Service) NotifyCompanyFrozen(ctx context.Context, companyID uuid.UUID, reason string) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT connection_uuid
		FROM integration_connections
		WHERE company_uuid = $1 AND provider = 'bitrix24' AND status <> 'revoked'
	`, companyID)
	if err != nil {
		return fmt.Errorf("list bitrix24 connections of a frozen company: %w", err)
	}

	connections := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			connections = append(connections, id)
		}
	}
	_ = rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}

	var failures error
	for _, connectionID := range connections {
		if err := s.postFrozenTask(ctx, connectionID, reason); err != nil {
			failures = errors.Join(failures, fmt.Errorf("connection %s: %w", connectionID, err))
		}
	}

	return failures
}

func (s *Service) postFrozenTask(ctx context.Context, connectionID uuid.UUID, reason string) error {
	info, token, err := s.pausedConnectionToken(ctx, connectionID)
	if err != nil {
		return err
	}
	if !hasTaskScope(info.Scopes) {
		return fmt.Errorf("connection has no task scope")
	}

	fields := map[string]any{
		"TITLE":       frozenTaskTitle,
		"DESCRIPTION": frozenTaskDescription(reason),
	}
	if info.ResponsibleID != "" {
		fields["RESPONSIBLE_ID"] = info.ResponsibleID
	}

	return s.call(ctx, info.Domain, "tasks.task.add", token, map[string]any{"fields": fields}, nil)
}

func frozenTaskDescription(reason string) string {
	switch reason {
	case "deletion":
		return "Компания в VerbaTrace удаляется, поэтому новые звонки из портала больше не обрабатываются. " +
			"Отмените удаление в VerbaTrace, чтобы вернуть обработку. Звонки, поступившие за это время, не потеряны."
	default:
		return "Компания в VerbaTrace заморожена, поэтому новые звонки из портала не обрабатываются. " +
			"Включите компанию в VerbaTrace, чтобы возобновить обработку. Звонки, поступившие за это время, не потеряны."
	}
}

// pausedConnectionToken reads the portal credentials whatever state the
// connection is in. The usual reader refuses a paused connection, and the one
// message worth sending is precisely the one that explains the pause.
func (s *Service) pausedConnectionToken(ctx context.Context, id uuid.UUID) (connectionInfo, string, error) {
	var settingsJSON string
	var ciphertext, nonce []byte
	var expires time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT c.settings::text, oc.access_token_ciphertext, oc.access_token_nonce, oc.expires_at
		FROM integration_connections c
		JOIN integration_oauth_credentials oc USING(connection_uuid)
		WHERE c.connection_uuid = $1 AND c.provider = 'bitrix24' AND oc.refresh_state = 'ready'
	`, id).Scan(&settingsJSON, &ciphertext, &nonce, &expires)
	if err != nil {
		return connectionInfo{}, "", err
	}
	if !expires.After(s.now().UTC().Add(30 * time.Second)) {
		if err = s.refreshConnectionToken(ctx, id); err != nil {
			return connectionInfo{}, "", err
		}
		return s.pausedConnectionToken(ctx, id)
	}

	plain, err := s.cipher.Decrypt(append(append([]byte{}, nonce...), ciphertext...), "integration_oauth_credentials/"+id.String()+"/access")
	if err != nil {
		return connectionInfo{}, "", err
	}

	var settings map[string]any
	_ = json.Unmarshal([]byte(settingsJSON), &settings)

	return connectionInfo{
		Domain:        stringValue(settings["portal_domain_display"]),
		Scopes:        stringSlice(settings["oauth_scope"]),
		ResponsibleID: stringValue(settings["oauth_user_external_id"]),
	}, string(plain), nil
}
