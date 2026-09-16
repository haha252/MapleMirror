package v2

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	Version         = "control.v2"
	MaxMessageBytes = 1 << 20
)

const (
	TypeSessionHello             = "session.hello"
	TypeSessionWelcome           = "session.welcome"
	TypeProtocolError            = "protocol.error"
	TypeSyncCancel               = "sync.cancel"
	TypeSyncResult               = "sync.result"
	TypeSyncResultAck            = "sync.result.ack"
	TypeSyncAccepted             = "sync.accepted"
	TypeSyncRejected             = "sync.rejected"
	TypeSyncTask                 = "sync.task"
	TypeDownloadAuthorization    = "download.authorization"
	TypeDownloadAuthorizationAck = "download.authorization.ack"
	TypeAuthorizationStatus      = "authorization.status"
	TypeAuthorizationStatusAck   = "authorization.status.ack"
	TypeTrafficEvent             = "traffic.event"
	TypeTrafficEventAck          = "traffic.event.ack"
	TypePublicProbeChallenge     = "public_probe.challenge"
	TypePublicProbeReady         = "public_probe.ready"
	TypeSwarmManifestReport      = "swarm.manifest.report"
	TypeSwarmManifestAck         = "swarm.manifest.ack"
	TypeSwarmSourcesRequest      = "swarm.sources.request"
	TypeSwarmSources             = "swarm.sources"
	TypeSwarmAvailability        = "swarm.availability"
	TypeInventorySnapshotSegment = "inventory.snapshot.segment"
	TypeInventorySnapshotAck     = "inventory.snapshot.ack"
	TypeNodeStatus               = "node.status"
)

type Envelope struct {
	Version string          `json:"v"`
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	ReplyTo string          `json:"reply_to,omitempty"`
	SentAt  time.Time       `json:"sent_at,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

func (e Envelope) Validate() error {
	if e.Version != Version {
		return fmt.Errorf("unsupported protocol version %q", e.Version)
	}
	if e.ID == "" || e.Type == "" {
		return errors.New("control message missing id or type")
	}
	if !Allowed(e.Type) {
		return fmt.Errorf("unsupported control message type %q", e.Type)
	}
	if len(e.Payload) == 0 {
		return errors.New("control message missing payload")
	}
	return nil
}

func New(messageType, id string, payload any) (Envelope, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Version: Version, Type: messageType, ID: id, SentAt: time.Now().UTC(), Payload: body}, nil
}

func Reply(messageType, id, replyTo string, payload any) (Envelope, error) {
	e, err := New(messageType, id, payload)
	e.ReplyTo = replyTo
	return e, err
}

func Decode[T any](e Envelope) (T, error) {
	var out T
	err := json.Unmarshal(e.Payload, &out)
	return out, err
}
