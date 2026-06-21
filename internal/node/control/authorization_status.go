package control

import (
	"database/sql"
	"encoding/json"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

func (c Client) sendNextAuthorizationStatusEvent(conn net.Conn, reqID string,
	sequence uint64) (uint64, bool, error) {
	if c.DB == nil {
		return sequence, false, nil
	}
	events, err := c.loadPendingAuthorizationStatusEvents(1)
	if err != nil || len(events) == 0 {
		return sequence, false, err
	}
	next, err := c.sendAuthorizationStatusEvent(conn, reqID, sequence, events[0])
	if err != nil {
		return sequence, false, err
	}
	return next, true, nil
}

func (c Client) loadPendingAuthorizationStatusEvents(limit int) ([]protocol.AuthorizationStatusEvent, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := c.DB.Query(`SELECT authorization_id, asset_id, status, reason,
		updated_at FROM local_authorizations
		WHERE reported_at IS NULL AND status != 'active'
		ORDER BY updated_at LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []protocol.AuthorizationStatusEvent
	for rows.Next() {
		event, err := scanAuthorizationStatus(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanAuthorizationStatus(rows *sql.Rows) (protocol.AuthorizationStatusEvent, error) {
	var event protocol.AuthorizationStatusEvent
	var occurred string
	err := rows.Scan(&event.AuthorizationID, &event.AssetID, &event.Status,
		&event.Reason, &occurred)
	if err != nil {
		return event, err
	}
	event.OccurredAt, _ = time.Parse(time.RFC3339Nano, occurred)
	return event, nil
}

func (c Client) sendAuthorizationStatusEvent(conn net.Conn, reqID string, sequence uint64,
	event protocol.AuthorizationStatusEvent) (uint64, error) {
	body, _ := json.Marshal(event)
	messageID := reqID + "-authorization-status"
	if err := c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: messageID,
		MessageType: protocol.TypeAuthorizationStatusEvent, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: sequence, Payload: body,
	}); err != nil {
		return sequence, err
	}
	next := sequence + 1
	_, err := c.readExpectedAck(conn, reqID, &next,
		protocol.TypeAuthorizationStatusAck, messageID)
	if err != nil {
		return sequence, err
	}
	_, err = c.DB.Exec(`UPDATE local_authorizations SET reported_at = ?
		WHERE authorization_id = ?`, time.Now().UTC().Format(time.RFC3339Nano),
		event.AuthorizationID)
	return next, err
}
