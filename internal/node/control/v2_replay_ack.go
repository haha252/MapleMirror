package control

import (
	"errors"
	"strconv"
	"time"

	"mirror-server/internal/controlv2"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func replayACKIdentity(e protocolv2.Envelope) (string, bool, error) {
	var fields []string
	switch e.Type {
	case protocolv2.TypeTrafficEventAck:
		ack, err := protocolv2.Decode[protocolv2.TrafficEventAck](e)
		if err != nil {
			return "", true, err
		}
		if ack.EventSequence == 0 {
			return "", true, errors.New("traffic ack missing sequence")
		}
		fields = []string{strconv.FormatUint(ack.EventSequence, 10)}
	case protocolv2.TypeAuthorizationStatusAck:
		ack, err := protocolv2.Decode[protocolv2.AuthorizationStatusAck](e)
		if err != nil {
			return "", true, err
		}
		fields = []string{ack.AuthorizationID, ack.Status}
	case protocolv2.TypeSyncResultAck:
		ack, err := protocolv2.Decode[protocolv2.SyncResultAck](e)
		if err != nil {
			return "", true, err
		}
		fields = []string{ack.TaskID, ack.AttemptID}
	case protocolv2.TypeSwarmManifestAck:
		ack, err := protocolv2.Decode[protocolv2.SwarmManifestAck](e)
		if err != nil {
			return "", true, err
		}
		fields = []string{ack.ManifestID, ack.AssetID}
	default:
		return "", false, nil
	}
	for _, field := range append(fields, e.ReplyTo) {
		if field == "" {
			return "", true, errors.New("durable ack missing correlation")
		}
	}
	return protocolv2.StableMessageID(e.Type, fields...), true, nil
}

func (c *Client) handleFrozenStatusACK(q *controlv2.Queue, e protocolv2.Envelope) (bool, error) {
	if e.Type != protocolv2.TypeAuthorizationStatusAck {
		return false, nil
	}
	original, present := q.ReplayEnvelope(e.ReplyTo)
	if !present || original.Type != protocolv2.TypeAuthorizationStatus {
		return false, nil
	}
	event, err := protocolv2.Decode[protocolv2.AuthorizationStatus](original)
	if err != nil {
		return true, err
	}
	ack, err := protocolv2.Decode[protocolv2.AuthorizationStatusAck](e)
	if err != nil {
		return true, err
	}
	if event.AuthorizationID != ack.AuthorizationID || event.Status != ack.Status {
		return true, errors.New("authorization status ack identity mismatch")
	}
	if c.DB == nil {
		return true, nil
	}
	// Confirm only the exact frozen version. A changed status remains pending,
	// while a valid ACK for its predecessor releases that predecessor's window.
	_, err = c.DB.ExecContext(c.controlContext(), `UPDATE local_authorizations SET reported_at=?
	 WHERE authorization_id=? AND status=? AND COALESCE(reason,'')=? AND updated_at=? AND reported_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), event.AuthorizationID, event.Status, event.Reason,
		event.OccurredAt.UTC().Format(time.RFC3339Nano))
	return true, err
}
