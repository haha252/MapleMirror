package control

import (
	"context"
	"errors"

	"mirror-server/internal/controlv2"
	"mirror-server/internal/protocol"
	protocolv2 "mirror-server/internal/protocol/v2"
)

func toV2Authorization(a protocol.DownloadAuthorization) protocolv2.DownloadAuthorization {
	return protocolv2.DownloadAuthorization{
		AuthorizationID: a.AuthorizationID, TokenHash: a.TokenHash, AssetID: a.AssetID,
		NodeID: a.NodeID, ClientPrefix: a.ClientPrefix, IssuedAt: a.IssuedAt, ExpiresAt: a.ExpiresAt,
		FirstConnectionSeconds: a.FirstConnectionSeconds, IdleTimeoutSeconds: a.IdleTimeoutSeconds,
		MaxDurationSeconds: a.MaxDurationSeconds, MaxBytes: a.MaxBytes,
		TrafficLimitBytes: a.TrafficLimitBytes, RangeConcurrencyLimit: a.RangeConcurrencyLimit,
		RequestID: a.RequestID,
	}
}

func (s *V2Server) dispatchV2Authorizations(ctx context.Context, session Session, queue *controlv2.Queue) error {
	return queue.Dispatch(func() error {
		for i := 0; i < controlv2.ReplayWindow; i++ {
			excluded := queue.ReplayKeys(protocolv2.TypeDownloadAuthorization)
			if len(excluded) >= controlv2.ReplayWindow {
				return nil
			}
			auth, ok, err := s.Repo.nextDownloadAuthorization(ctx, session.NodeID, excluded)
			if err != nil || !ok {
				return err
			}
			envelope, err := protocolv2.New(protocolv2.TypeDownloadAuthorization, protocolv2.StableMessageID(protocolv2.TypeDownloadAuthorization, auth.AuthorizationID), toV2Authorization(auth))
			if err != nil {
				return err
			}
			if err := queue.EnqueueReplay(envelope, auth.AuthorizationID); err != nil {
				if errors.Is(err, controlv2.ErrReplayWindowFull) || errors.Is(err, controlv2.ErrQueueFull) {
					return nil
				}
				return err
			}
		}
		return nil
	})
}

func (s *V2Server) maybeDispatchV2PublicProbe(session Session, queue *controlv2.Queue) error {
	if s.PublicProbes == nil {
		return nil
	}
	challenge := s.PublicProbes.ChallengeForHeartbeat(session.NodeID)
	if challenge == nil {
		return nil
	}
	body := protocolv2.PublicProbeChallenge{ChallengeID: challenge.ChallengeID, Nonce: challenge.Nonce,
		ExpiresAt: challenge.ExpiresAt, Algorithm: challenge.Algorithm}
	envelope, err := protocolv2.New(protocolv2.TypePublicProbeChallenge, protocolv2.StableMessageID(protocolv2.TypePublicProbeChallenge, challenge.ChallengeID), body)
	if err != nil {
		return err
	}
	return queue.Enqueue(envelope, session.NodeID)
}
