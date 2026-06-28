package control

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"time"

	"mirror-server/internal/protocol"
)

func (s ControlServer) sessionReadTimeout() time.Duration {
	if s.HeartbeatTimeout > 0 {
		return s.HeartbeatTimeout + s.HeartbeatInterval + controlWriteTimeout
	}
	return 45 * time.Second
}

func (s ControlServer) writeStartSessionReject(conn net.Conn, tlsConn *tls.Conn, reqID, fingerprint string, err error) {
	code := "CONTROL_INTERNAL_ERROR"
	message := err.Error()
	switch {
	case errors.Is(err, ErrCertificateNotActive):
		code = "CERTIFICATE_NOT_ACTIVE"
	}
	nodeID := rejectNodeID(s, tlsConn, fingerprint)
	s.writeProtocolError(conn, nodeID, reqID, "", code, message)
}

func (s ControlServer) writeProtocolError(conn net.Conn, nodeID, reqID, replyTo, code, message string) {
	body, _ := json.Marshal(protocol.ProtocolError{Code: code, Message: message})
	_ = writeControlFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version,
		MessageID:       reqID,
		MessageType:     protocol.TypeProtocolError,
		SentAt:          time.Now().UTC(),
		NodeID:          nodeID,
		RequestID:       reqID,
		ReplyTo:         replyTo,
		Payload:         body,
	})
}

func rejectNodeID(s ControlServer, tlsConn *tls.Conn, fingerprint string) string {
	if tlsConn != nil && len(tlsConn.ConnectionState().PeerCertificates) > 0 {
		if nodeID := tlsConn.ConnectionState().PeerCertificates[0].Subject.CommonName; nodeID != "" {
			return nodeID
		}
	}
	if fingerprint != "" {
		if resolved, err := s.Repo.NodeIDByFingerprint(context.Background(), fingerprint); err == nil {
			return resolved
		}
	}
	return fingerprint
}
