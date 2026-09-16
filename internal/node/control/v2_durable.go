package control

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"mirror-server/internal/controlv2"
	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/requestid"
)

func (c *Client) enqueueV2Durable(queue *controlv2.Queue) error {
	if err := c.enqueuePendingV2Manifests(queue); err != nil {
		return err
	}
	if err := c.enqueuePendingV2Results(queue); err != nil {
		return err
	}
	if err := c.enqueuePendingV2Traffic(queue); err != nil {
		return err
	}
	if err := c.enqueuePendingV2AuthorizationStatus(queue); err != nil {
		return err
	}
	return nil
}

func (c *Client) enqueuePendingV2Traffic(queue *controlv2.Queue) error {
	if c.DB == nil {
		return nil
	}
	events, err := c.loadPendingTrafficEvents(50)
	if err != nil {
		return err
	}
	for _, event := range events {
		body := protocolv2.TrafficEvent{
			EventSequence: event.EventSequence, AuthorizationID: event.AuthorizationID, AssetID: event.AssetID,
			NodeRequestID: event.NodeRequestID, MasterRequestID: event.MasterRequestID, SentBytes: event.SentBytes,
			Status: event.Status, ReportedAt: event.ReportedAt,
		}
		id := protocolv2.StableMessageID(protocolv2.TypeTrafficEvent, strconv.FormatUint(event.EventSequence, 10))
		envelope, _ := protocolv2.New(protocolv2.TypeTrafficEvent, id, body)
		if err := queue.Enqueue(envelope, ""); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) handleV2TrafficAck(envelope protocolv2.Envelope) error {
	ack, err := protocolv2.Decode[protocolv2.TrafficEventAck](envelope)
	if err != nil {
		return err
	}
	if ack.EventSequence == 0 {
		return errors.New("traffic ack missing correlation")
	}
	expectedReplyTo := protocolv2.StableMessageID(protocolv2.TypeTrafficEvent, strconv.FormatUint(ack.EventSequence, 10))
	if envelope.ReplyTo != expectedReplyTo {
		return errors.New("traffic ack reply_to mismatch")
	}
	if c.DB == nil {
		return nil
	}
	res, err := c.DB.Exec(`UPDATE pending_traffic_events SET confirmed_at=? WHERE event_sequence=? AND confirmed_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), ack.EventSequence)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	} // duplicate ACK is idempotent.
	return nil
}

func (c *Client) enqueuePendingV2AuthorizationStatus(queue *controlv2.Queue) error {
	if c.DB == nil {
		return nil
	}
	events, err := c.loadPendingAuthorizationStatusEvents(50)
	if err != nil {
		return err
	}
	for _, event := range events {
		body := protocolv2.AuthorizationStatus{AuthorizationID: event.AuthorizationID, AssetID: event.AssetID,
			Status: event.Status, Reason: event.Reason, OccurredAt: event.OccurredAt}
		id := protocolv2.StableMessageID(protocolv2.TypeAuthorizationStatus, event.AuthorizationID, event.Status, event.Reason, event.OccurredAt.UTC().Format(time.RFC3339Nano))
		envelope, _ := protocolv2.New(protocolv2.TypeAuthorizationStatus, id, body)
		if err := queue.Enqueue(envelope, ""); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) handleV2AuthorizationStatusAck(envelope protocolv2.Envelope) error {
	ack, err := protocolv2.Decode[protocolv2.AuthorizationStatusAck](envelope)
	if err != nil {
		return err
	}
	if ack.AuthorizationID == "" || ack.Status == "" {
		return errors.New("authorization status ack missing correlation")
	}
	if c.DB == nil {
		return nil
	}
	var reason, updatedAt string
	if err := c.DB.QueryRow(`SELECT COALESCE(reason,''), updated_at FROM local_authorizations
		WHERE authorization_id=? AND status=? AND reported_at IS NULL`, ack.AuthorizationID, ack.Status).Scan(&reason, &updatedAt); err != nil {
		return err
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return err
	}
	expectedReplyTo := protocolv2.StableMessageID(protocolv2.TypeAuthorizationStatus, ack.AuthorizationID, ack.Status, reason, occurredAt.UTC().Format(time.RFC3339Nano))
	if envelope.ReplyTo != expectedReplyTo {
		return errors.New("authorization status ack reply_to mismatch")
	}
	res, err := c.DB.Exec(`UPDATE local_authorizations SET reported_at=?
		WHERE authorization_id=? AND status=? AND COALESCE(reason,'')=? AND updated_at=? AND reported_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339Nano), ack.AuthorizationID, ack.Status, reason, updatedAt)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("authorization status ack no longer matches pending event")
	}
	return nil
}

func (c *Client) handleV2DownloadAuthorization(queue *controlv2.Queue, envelope protocolv2.Envelope) error {
	auth, err := protocolv2.Decode[protocolv2.DownloadAuthorization](envelope)
	if err != nil {
		return err
	}
	if envelope.ID != protocolv2.StableMessageID(protocolv2.TypeDownloadAuthorization, auth.AuthorizationID) {
		return errors.New("download.authorization message id mismatch")
	}
	legacy := protocol.DownloadAuthorization{
		AuthorizationID: auth.AuthorizationID, TokenHash: auth.TokenHash, AssetID: auth.AssetID, NodeID: auth.NodeID,
		ClientPrefix: auth.ClientPrefix, IssuedAt: auth.IssuedAt, ExpiresAt: auth.ExpiresAt,
		FirstConnectionSeconds: auth.FirstConnectionSeconds, IdleTimeoutSeconds: auth.IdleTimeoutSeconds,
		MaxDurationSeconds: auth.MaxDurationSeconds, MaxBytes: auth.MaxBytes, TrafficLimitBytes: auth.TrafficLimitBytes,
		RangeConcurrencyLimit: auth.RangeConcurrencyLimit, RequestID: auth.RequestID,
	}
	if err := c.storeDownloadAuthorization(legacy); err != nil {
		return err
	}
	id, _ := requestid.New()
	ack, _ := protocolv2.Reply(protocolv2.TypeDownloadAuthorizationAck, id, envelope.ID,
		protocolv2.DownloadAuthorizationAck{AuthorizationID: auth.AuthorizationID})
	return queue.Enqueue(ack, "")
}

func (c *Client) handleV2PublicProbe(queue *controlv2.Queue, envelope protocolv2.Envelope) error {
	challenge, err := protocolv2.Decode[protocolv2.PublicProbeChallenge](envelope)
	if err != nil {
		return err
	}
	if envelope.ID != protocolv2.StableMessageID(protocolv2.TypePublicProbeChallenge, challenge.ChallengeID) {
		return errors.New("public_probe.challenge message id mismatch")
	}
	if c.ProbeStore == nil {
		return nil
	}
	legacy := protocol.PublicProbeChallenge{ChallengeID: challenge.ChallengeID, Nonce: challenge.Nonce,
		ExpiresAt: challenge.ExpiresAt, Algorithm: challenge.Algorithm}
	if err := c.ProbeStore.Accept(legacy); err != nil {
		return err
	}
	id, _ := requestid.New()
	ready, _ := protocolv2.Reply(protocolv2.TypePublicProbeReady, id, envelope.ID,
		protocolv2.PublicProbeReady{ChallengeID: challenge.ChallengeID})
	return queue.Enqueue(ready, "")
}

func trafficKey(seq uint64) string         { return "traffic:" + strconv.FormatUint(seq, 10) }
func statusKey(auth, status string) string { return fmt.Sprintf("auth:%s:%s", auth, status) }
