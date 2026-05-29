package control

import (
	"context"
	"encoding/json"
	"net"
	"time"

	"mirror-server/internal/protocol"
	"mirror-server/internal/requestid"
)

type EnrollmentServer struct {
	Repo                      Repository
	EnrollmentTimeout         time.Duration
	DownloadTokenPublicKeyPEM string
	MasterCAPEM               string
}

func (s EnrollmentServer) Handle(conn net.Conn) {
	defer conn.Close()
	reqID, err := requestid.New()
	if err != nil {
		return
	}
	msg, err := protocol.ReadFrame(conn, protocol.MaxFrameBytes)
	if err != nil {
		return
	}
	if err := msg.Validate(protocol.Enrollment); err != nil {
		_ = writeProtocolError(conn, msg, reqID, "INVALID_PAYLOAD", err.Error())
		return
	}
	switch msg.MessageType {
	case protocol.TypeEnrollRequest:
		s.handleEnrollRequest(conn, msg, reqID)
	case protocol.TypeEnrollCertificate:
		s.handleCertificateCollect(conn, msg, reqID)
	default:
		_ = writeProtocolError(conn, msg, reqID, "INVALID_PAYLOAD", "登记入口只接受登记和领证请求")
	}
}

func (s EnrollmentServer) handleEnrollRequest(conn net.Conn, msg protocol.Envelope, reqID string) {
	var payload protocol.EnrollRequest
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		_ = writeProtocolError(conn, msg, reqID, "INVALID_PAYLOAD", "登记请求内容不合法")
		return
	}
	caps, _ := json.Marshal(payload.Capabilities)
	enrollment, err := s.Repo.CreateEnrollment(context.Background(), payload.PairingCode,
		payload.PublicName, payload.CSRPem, payload.PublicKeyFingerprint,
		string(caps), reqID, s.EnrollmentTimeout)
	if err != nil {
		_ = writeProtocolError(conn, msg, reqID, "PAIRING_CODE_INVALID", err.Error())
		return
	}
	body, _ := json.Marshal(protocol.EnrollPending{
		EnrollmentID: enrollment.ID, ExpiresAt: enrollment.ExpiresAt,
		RetryAfterSeconds: 10, Message: "登记请求已提交，等待管理员审批",
	})
	_ = protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID,
		MessageType: protocol.TypeEnrollPending, SentAt: time.Now().UTC(),
		RequestID: reqID, ReplyTo: msg.MessageID, Payload: body,
	})
}

func (s EnrollmentServer) handleCertificateCollect(conn net.Conn, msg protocol.Envelope, reqID string) {
	var payload protocol.EnrollCertificateRequest
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		_ = writeProtocolError(conn, msg, reqID, "INVALID_PAYLOAD", "领证请求内容不合法")
		return
	}
	delivery, err := s.Repo.CollectCertificate(context.Background(), payload.EnrollmentID)
	if err != nil {
		_ = writeProtocolError(conn, msg, reqID, "ENROLLMENT_PENDING", err.Error())
		return
	}
	body, _ := json.Marshal(protocol.EnrollCertificate{
		EnrollmentID: delivery.EnrollmentID, NodeID: delivery.NodeID,
		CertificatePEM: delivery.CertificatePEM, CAChainPEM: s.MasterCAPEM,
		DownloadTokenPublicKeyPEM: s.DownloadTokenPublicKeyPEM,
		NotAfter:                  delivery.NotAfter,
	})
	_ = protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID,
		MessageType: protocol.TypeEnrollCertificate, SentAt: time.Now().UTC(),
		RequestID: reqID, ReplyTo: msg.MessageID, Payload: body,
	})
}

func writeProtocolError(conn net.Conn, msg protocol.Envelope, reqID, code, text string) error {
	body, _ := json.Marshal(map[string]string{"code": code, "message": text})
	return protocol.WriteFrame(conn, protocol.Envelope{
		ProtocolVersion: protocol.Version, MessageID: reqID,
		MessageType: protocol.TypeProtocolError, SentAt: time.Now().UTC(),
		RequestID: reqID, ReplyTo: msg.MessageID, Payload: body,
	})
}
