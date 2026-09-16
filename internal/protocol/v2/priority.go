package v2

import "fmt"

const (
	PriorityHighest = 1
	PriorityLowest  = 128
)

type MessageSpec struct {
	Priority    uint8
	Coalescible bool
}

var registry = map[string]MessageSpec{
	TypeSessionHello:             {Priority: 1},
	TypeSessionWelcome:           {Priority: 1},
	TypeProtocolError:            {Priority: 2},
	TypeSyncCancel:               {Priority: 4, Coalescible: true},
	TypeSyncResult:               {Priority: 8},
	TypeSyncResultAck:            {Priority: 9},
	TypeSyncAccepted:             {Priority: 10, Coalescible: true},
	TypeSyncRejected:             {Priority: 10, Coalescible: true},
	TypeSyncTask:                 {Priority: 12, Coalescible: true},
	TypeDownloadAuthorization:    {Priority: 16, Coalescible: true},
	TypeDownloadAuthorizationAck: {Priority: 17},
	TypeAuthorizationStatus:      {Priority: 20},
	TypeAuthorizationStatusAck:   {Priority: 21},
	TypeTrafficEvent:             {Priority: 24},
	TypeTrafficEventAck:          {Priority: 25},
	TypePublicProbeChallenge:     {Priority: 28, Coalescible: true},
	TypePublicProbeReady:         {Priority: 29},
	TypeSwarmManifestReport:      {Priority: 33, Coalescible: true},
	TypeSwarmManifestAck:         {Priority: 34},
	TypeSwarmSourcesRequest:      {Priority: 36, Coalescible: true},
	TypeSwarmSources:             {Priority: 38, Coalescible: true},
	TypeSwarmAvailability:        {Priority: 40, Coalescible: true},
	TypeInventorySnapshotSegment: {Priority: 60},
	TypeInventorySnapshotAck:     {Priority: 61},
	TypeNodeStatus:               {Priority: 76, Coalescible: true},
}

func Allowed(messageType string) bool {
	_, ok := registry[messageType]
	return ok
}

func Spec(messageType string) (MessageSpec, bool) {
	s, ok := registry[messageType]
	return s, ok
}

func Priority(messageType string) (uint8, error) {
	s, ok := registry[messageType]
	if !ok {
		return 0, fmt.Errorf("unregistered control.v2 message type %q", messageType)
	}
	return s.Priority, nil
}

func RegisteredTypes() map[string]MessageSpec {
	out := make(map[string]MessageSpec, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}
