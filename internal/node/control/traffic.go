package control

import (
	"database/sql"
	"encoding/json"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

func (c Client) sendPendingTraffic(conn net.Conn, reqID string, sequence uint64) (uint64, error) {
	if c.DB == nil {
		return sequence, nil
	}
	events, err := c.loadPendingTrafficEvents(20)
	if err != nil {
		return sequence, err
	}
	for _, event := range events {
		next, err := c.sendTrafficEvent(conn, reqID, sequence, event)
		if err != nil {
			return sequence, err
		}
		sequence = next
	}
	return sequence, nil
}

func (c Client) sendNextTrafficEvent(conn net.Conn, reqID string,
	sequence uint64) (uint64, bool, error) {
	if c.DB == nil {
		return sequence, false, nil
	}
	events, err := c.loadPendingTrafficEvents(1)
	if err != nil || len(events) == 0 {
		return sequence, false, err
	}
	next, err := c.sendTrafficEvent(conn, reqID, sequence, events[0])
	if err != nil {
		return sequence, false, err
	}
	return next, true, nil
}

func (c Client) loadPendingTrafficEvents(limit int) ([]protocol.TrafficEvent, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := c.DB.Query(`SELECT event_sequence, authorization_id, node_request_id,
		master_request_id, sent_bytes, created_at, asset_id, status
		FROM pending_traffic_events WHERE confirmed_at IS NULL
		ORDER BY event_sequence LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []protocol.TrafficEvent
	for rows.Next() {
		event, err := scanTraffic(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanTraffic(rows *sql.Rows) (protocol.TrafficEvent, error) {
	var event protocol.TrafficEvent
	var created string
	err := rows.Scan(&event.EventSequence, &event.AuthorizationID,
		&event.NodeRequestID, &event.MasterRequestID, &event.SentBytes,
		&created, &event.AssetID, &event.Status)
	if err != nil {
		return event, err
	}
	event.ReportedAt, _ = time.Parse(time.RFC3339Nano, created)
	return event, nil
}

func (c Client) sendTrafficEvent(conn net.Conn, reqID string, sequence uint64, event protocol.TrafficEvent) (uint64, error) {
	body, _ := json.Marshal(event)
	if err := c.writeFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID + "-traffic",
		MessageType: protocol.TypeTrafficEvent, SentAt: time.Now().UTC(),
		NodeID: c.NodeID, RequestID: reqID, Sequence: sequence, Payload: body,
	}); err != nil {
		return sequence, err
	}
	next := sequence + 1
	_, err := c.readExpectedResponse(conn, reqID, &next, protocol.TypeTrafficEventAck)
	if err != nil {
		return sequence, err
	}
	_, err = c.DB.Exec(`UPDATE pending_traffic_events SET confirmed_at = ?
		WHERE event_sequence = ?`, time.Now().UTC().Format(time.RFC3339Nano),
		event.EventSequence)
	return next, err
}
