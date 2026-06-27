package control

import "mirror-server/internal/protocol"

func runOnceExpectPeerClose(client *Client) error {
	_, err := client.RunOnce()
	if runOnceEndedByPeer(err) {
		return nil
	}
	return err
}

type publicProbeAcceptFunc func(protocol.PublicProbeChallenge) error

func (f publicProbeAcceptFunc) Accept(challenge protocol.PublicProbeChallenge) error {
	return f(challenge)
}
