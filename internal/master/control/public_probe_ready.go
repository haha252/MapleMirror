package control

import "context"

func (r Repository) AcceptPublicProbeReady(ctx context.Context,
	session Session, seq uint64) (HeartbeatResult, error) {
	last, err := r.currentSequence(session)
	if err != nil {
		return HeartbeatResult{}, err
	}
	ready := r.nodeRoutingReady(ctx, session.NodeID)
	if seq <= last {
		return HeartbeatResult{
			AcceptedSequence: last,
			ManagedState:     managedState(ready),
			RoutingReady:     ready,
		}, nil
	}
	if err := r.updateSequence(session, seq); err != nil {
		return HeartbeatResult{}, err
	}
	return HeartbeatResult{
		AcceptedSequence: seq,
		ManagedState:     managedState(ready),
		RoutingReady:     ready,
	}, nil
}
