package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"mirror-server/internal/protocol"
	"mirror-server/internal/requestid"
)

type enrollmentWSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func (s EnrollmentServer) WebSocketHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/enroll/v2", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		conn.SetReadLimit(protocol.MaxFrameBytes)
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		messageType, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		if messageType != websocket.MessageText {
			_ = writeEnrollmentWS(ctx, conn, protocol.TypeProtocolError,
				map[string]string{"code": "INVALID_PAYLOAD", "message": "enrollment.v2 requires text JSON"})
			return
		}
		var msg enrollmentWSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			_ = writeEnrollmentWS(ctx, conn, protocol.TypeProtocolError,
				map[string]string{"code": "INVALID_PAYLOAD", "message": "invalid JSON"})
			return
		}
		replyType, payload := s.handleEnrollmentWS(ctx, msg)
		if err := writeEnrollmentWS(ctx, conn, replyType, payload); err != nil {
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "enrollment RPC complete")
	})
	return mux
}

func (s EnrollmentServer) handleEnrollmentWS(ctx context.Context, msg enrollmentWSMessage) (string, any) {
	reqID, _ := requestid.New()
	switch msg.Type {
	case protocol.TypeEnrollRequest:
		var payload protocol.EnrollRequest
		if json.Unmarshal(msg.Payload, &payload) != nil {
			return protocol.TypeProtocolError, map[string]string{"code": "INVALID_PAYLOAD", "message": "invalid enrollment request"}
		}
		caps, _ := json.Marshal(payload.Capabilities)
		enrollment, err := s.Repo.CreateEnrollment(ctx, payload.PairingCode, payload.PublicName,
			payload.CSRPem, payload.PublicKeyFingerprint, string(caps), reqID, s.EnrollmentTimeout)
		if err != nil {
			return protocol.TypeProtocolError, map[string]string{"code": "PAIRING_CODE_INVALID", "message": err.Error()}
		}
		return protocol.TypeEnrollPending, protocol.EnrollPending{EnrollmentID: enrollment.ID,
			ExpiresAt: enrollment.ExpiresAt, RetryAfterSeconds: 10, Message: "登记请求已提交，等待管理员审批"}
	case protocol.TypeEnrollCertificate:
		var payload protocol.EnrollCertificateRequest
		if json.Unmarshal(msg.Payload, &payload) != nil {
			return protocol.TypeProtocolError, map[string]string{"code": "INVALID_PAYLOAD", "message": "invalid certificate request"}
		}
		delivery, err := s.Repo.CollectCertificate(ctx, payload.EnrollmentID)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return protocol.TypeProtocolError, map[string]string{"code": "TEMPORARY_ERROR", "message": err.Error()}
			}
			return protocol.TypeProtocolError, map[string]string{"code": "ENROLLMENT_PENDING", "message": err.Error()}
		}
		return protocol.TypeEnrollCertificate, protocol.EnrollCertificate{EnrollmentID: delivery.EnrollmentID,
			NodeID: delivery.NodeID, CertificatePEM: delivery.CertificatePEM, CAChainPEM: s.MasterCAPEM,
			DownloadTokenPublicKeyPEM: s.DownloadTokenPublicKeyPEM, NotAfter: delivery.NotAfter}
	default:
		return protocol.TypeProtocolError, map[string]string{"code": "INVALID_PAYLOAD", "message": "unsupported enrollment message"}
	}
}

func writeEnrollmentWS(ctx context.Context, conn *websocket.Conn, messageType string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data, err := json.Marshal(enrollmentWSMessage{Type: messageType, Payload: body})
	if err != nil {
		return err
	}
	if len(data) > protocol.MaxFrameBytes {
		return errors.New("enrollment.v2 response exceeds hard limit")
	}
	return conn.Write(ctx, websocket.MessageText, data)
}
