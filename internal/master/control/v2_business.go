package control

import (
	"context"
	"errors"
	"strconv"
	"time"

	"mirror-server/internal/controlv2"
	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/swarm"
)

func (s *V2Server) handleV2BusinessMessage(ctx context.Context, session Session, queue *controlv2.Queue, envelope protocolv2.Envelope) error {
	switch envelope.Type {
	case protocolv2.TypeSyncAccepted:
		accepted, err := protocolv2.Decode[protocolv2.SyncAccepted](envelope)
		if err != nil {
			return err
		}
		if err := requireV2ReplyTo(envelope, protocolv2.TypeSyncTask, accepted.TaskID, accepted.AttemptID); err != nil {
			return err
		}
		_, err = s.Repo.acceptV2Task(ctx, session, accepted.TaskID, accepted.AttemptID)
		if err != nil {
			return err
		}
		return s.dispatchV2Tasks(ctx, session, queue)
	case protocolv2.TypeSyncRejected:
		rejected, err := protocolv2.Decode[protocolv2.SyncRejected](envelope)
		if err != nil {
			return err
		}
		if err := requireV2ReplyTo(envelope, protocolv2.TypeSyncTask, rejected.TaskID, rejected.AttemptID); err != nil {
			return err
		}
		if err := s.Repo.rejectV2Task(ctx, session, rejected); err != nil {
			return err
		}
		return s.dispatchV2Tasks(ctx, session, queue)
	case protocolv2.TypeSyncResult:
		result, err := protocolv2.Decode[protocolv2.SyncResult](envelope)
		if err != nil {
			return err
		}
		if err := requireV2MessageID(envelope, protocolv2.TypeSyncResult, result.TaskID, result.AttemptID); err != nil {
			return err
		}
		processed, err := s.Repo.acceptV2SyncResult(ctx, session, result)
		if err != nil {
			return err
		}
		ack, _ := protocolv2.Reply(protocolv2.TypeSyncResultAck, mustID(), envelope.ID,
			protocolv2.SyncResultAck{TaskID: result.TaskID, AttemptID: result.AttemptID, Accepted: processed})
		if err := queue.Enqueue(ack, ""); err != nil {
			return err
		}
		return s.dispatchV2Tasks(ctx, session, queue)
	case protocolv2.TypeSwarmManifestReport:
		manifest, err := protocolv2.Decode[protocolv2.SwarmManifest](envelope)
		if err != nil {
			return err
		}
		if err := requireV2MessageID(envelope, protocolv2.TypeSwarmManifestReport, manifest.ManifestID); err != nil {
			return err
		}
		ackBody, err := s.Repo.AcceptV2Manifest(ctx, session.NodeID, manifest)
		if err != nil {
			return err
		}
		ack, _ := protocolv2.Reply(protocolv2.TypeSwarmManifestAck, mustID(), envelope.ID, ackBody)
		if err := queue.Enqueue(ack, ""); err != nil {
			return err
		}
		switch ackBody.Status {
		case "accepted":
			s.pushV2SourcesForAsset(ctx, manifest.AssetID)
		case "conflict":
			return s.freezeV2SwarmAsset(ctx, manifest.AssetID)
		}
		return nil
	case protocolv2.TypeSwarmAvailability:
		a, err := protocolv2.Decode[protocolv2.SwarmAvailability](envelope)
		if err != nil {
			return err
		}
		m, ok, err := s.Repo.LoadV2Manifest(ctx, a.AssetID)
		if err != nil {
			return err
		}
		if !ok || m.ManifestID != a.ManifestID {
			return nil
		}
		if len(a.Bitset) != swarm.BitsetBytes(m.PieceCount) {
			return errors.New("invalid swarm availability bitset")
		}
		s.Repo.runtime().UpdateSwarmAvailability(session.NodeID, a)
		s.pushV2SourcesForAsset(ctx, a.AssetID)
		return nil
	case protocolv2.TypeSwarmSourcesRequest:
		req, err := protocolv2.Decode[protocolv2.SwarmSourcesRequest](envelope)
		if err != nil {
			return err
		}
		if err := requireV2MessageID(envelope, protocolv2.TypeSwarmSourcesRequest, req.TaskID, req.AttemptID, req.AssetID, req.ManifestID); err != nil {
			return err
		}
		if err := s.Repo.validateV2SwarmSourceRequest(ctx, session.NodeID, req); err != nil {
			return err
		}
		m, ok, err := s.Repo.LoadV2Manifest(ctx, req.AssetID)
		if err != nil {
			return err
		}
		if !ok || m.ManifestID != req.ManifestID {
			return nil
		}
		sources, err := s.Repo.v2SwarmSources(ctx, session.NodeID, m)
		if err != nil {
			return err
		}
		body := protocolv2.SwarmSources{TaskID: req.TaskID, AttemptID: req.AttemptID, AssetID: req.AssetID, ManifestID: req.ManifestID, Sources: sources}
		env, _ := protocolv2.Reply(protocolv2.TypeSwarmSources, mustID(), envelope.ID, body)
		return queue.Enqueue(env, req.AssetID+"/"+req.AttemptID)
	case protocolv2.TypeInventorySnapshotSegment:
		segment, err := protocolv2.Decode[protocolv2.InventorySnapshotSegment](envelope)
		if err != nil {
			return err
		}
		if err := requireV2MessageID(envelope, protocolv2.TypeInventorySnapshotSegment, session.NodeID, strconv.FormatUint(segment.Revision, 10), strconv.Itoa(segment.Segment)); err != nil {
			return err
		}
		complete, err := s.Repo.AcceptV2InventorySegment(ctx, session, segment, envelope.ID)
		if err != nil {
			return err
		}
		if complete {
			ack, _ := protocolv2.Reply(protocolv2.TypeInventorySnapshotAck, mustID(), envelope.ID, protocolv2.InventorySnapshotAck{Revision: segment.Revision})
			if err := queue.Enqueue(ack, ""); err != nil {
				return err
			}
		}
		return s.dispatchV2Tasks(ctx, session, queue)
	case protocolv2.TypeDownloadAuthorizationAck:
		ack, err := protocolv2.Decode[protocolv2.DownloadAuthorizationAck](envelope)
		if err != nil {
			return err
		}
		if err := requireV2ReplyTo(envelope, protocolv2.TypeDownloadAuthorization, ack.AuthorizationID); err != nil {
			return err
		}
		if err := s.Repo.AcceptV2DownloadAuthorizationAck(ctx, session, ack.AuthorizationID); err != nil {
			return err
		}
		queue.Acknowledge(envelope.ReplyTo)
		return nil
	case protocolv2.TypeTrafficEvent:
		event, err := protocolv2.Decode[protocolv2.TrafficEvent](envelope)
		if err != nil {
			return err
		}
		if err := requireV2MessageID(envelope, protocolv2.TypeTrafficEvent, strconv.FormatUint(event.EventSequence, 10)); err != nil {
			return err
		}
		dup, err := s.Repo.AcceptV2TrafficEvent(ctx, session, event)
		if err != nil {
			return err
		}
		ack, _ := protocolv2.Reply(protocolv2.TypeTrafficEventAck, mustID(), envelope.ID, protocolv2.TrafficEventAck{EventSequence: event.EventSequence, Duplicate: dup})
		return queue.Enqueue(ack, "")
	case protocolv2.TypeAuthorizationStatus:
		event, err := protocolv2.Decode[protocolv2.AuthorizationStatus](envelope)
		if err != nil {
			return err
		}
		if err := requireV2MessageID(envelope, protocolv2.TypeAuthorizationStatus, event.AuthorizationID, event.Status, event.Reason, event.OccurredAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		if err := s.Repo.AcceptV2AuthorizationStatus(ctx, session, event); err != nil {
			return err
		}
		ack, _ := protocolv2.Reply(protocolv2.TypeAuthorizationStatusAck, mustID(), envelope.ID, protocolv2.AuthorizationStatusAck{AuthorizationID: event.AuthorizationID, Status: event.Status})
		return queue.Enqueue(ack, "")
	case protocolv2.TypePublicProbeReady:
		ready, err := protocolv2.Decode[protocolv2.PublicProbeReady](envelope)
		if err != nil {
			return err
		}
		if err := requireV2ReplyTo(envelope, protocolv2.TypePublicProbeChallenge, ready.ChallengeID); err != nil {
			return err
		}
		if s.PublicProbes != nil {
			s.PublicProbes.AcceptReady(session.NodeID, protocol.PublicProbeReady{ChallengeID: ready.ChallengeID})
		}
		return nil
	default:
		return errors.New("unsupported control.v2 business message: " + envelope.Type)
	}
}
