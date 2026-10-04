package control

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"

	"mirror-server/internal/controltls"
	"mirror-server/internal/controlv2"
	"mirror-server/internal/logging"
	protocolv2 "mirror-server/internal/protocol/v2"
	"mirror-server/internal/requestid"
)

type V2Server struct {
	Repo             Repository
	StatusInterval   time.Duration
	HeartbeatTimeout time.Duration
	Logger           *logging.Logger
	PublicProbes     *PublicProbeService
	registry         sync.Map // node_id -> *websocket.Conn
	queues           sync.Map // node_id -> *controlv2.Queue
}

func (s *V2Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/control/v2", s.handle)
	return mux
}

func (s *V2Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		http.Error(w, "client certificate required", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	conn.SetReadLimit(protocolv2.MaxMessageBytes)
	ctx := r.Context()
	fingerprint := controltls.Fingerprint(r.TLS.PeerCertificates[0])

	// Validate the application hello before creating/taking over the durable
	// control session. A TLS-authenticated but malformed new connection must not
	// evict a healthy session.
	helloCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	helloEnvelope, err := readV2Envelope(helloCtx, conn)
	cancel()
	if err != nil || helloEnvelope.Type != protocolv2.TypeSessionHello {
		s.writeDirectError(ctx, conn, "expected_hello", "first control.v2 message must be session.hello")
		_ = conn.Close(websocket.StatusPolicyViolation, "invalid hello")
		return
	}
	hello, err := protocolv2.Decode[protocolv2.Hello](helloEnvelope)
	if err != nil {
		s.writeDirectError(ctx, conn, "invalid_hello", err.Error())
		_ = conn.Close(websocket.StatusPolicyViolation, "invalid hello")
		return
	}

	reqID, _ := requestid.New()
	session, err := s.Repo.StartSession(ctx, fingerprint, reqID)
	if err != nil {
		code := "CERTIFICATE_REJECTED"
		if errors.Is(err, ErrCertificateNotActive) {
			code = "CERTIFICATE_NOT_ACTIVE"
		}
		s.writeDirectError(ctx, conn, code, err.Error())
		_ = conn.Close(websocket.StatusPolicyViolation, "certificate rejected")
		return
	}
	closeReason := "control.v2 websocket closed"
	defer func() {
		s.registry.CompareAndDelete(session.NodeID, conn)
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = conn.CloseNow()
		_ = s.Repo.CloseSession(closeCtx, session.ID, closeReason)
	}()
	if old, loaded := s.registry.Swap(session.NodeID, conn); loaded {
		if previous, ok := old.(*websocket.Conn); ok && previous != conn {
			_ = previous.CloseNow()
		}
	}

	s.Repo.runtime().SetControlProtocol(session.NodeID, "v2")
	s.Repo.runtime().SetSoftwareVersion(session.NodeID, hello.SoftwareVersion)
	peerOnly := false
	for _, capability := range hello.Capabilities {
		if capability == "sync.peer_only.v1" {
			peerOnly = true
			break
		}
	}
	s.Repo.runtime().SetPeerOnly(session.NodeID, peerOnly)
	ready := s.Repo.nodeRoutingReady(ctx, session.NodeID)
	interval := s.StatusInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	timeout := s.HeartbeatTimeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	welcome, _ := protocolv2.Reply(protocolv2.TypeSessionWelcome, mustID(), helloEnvelope.ID, protocolv2.Welcome{
		SessionID: session.ID, ServerTime: time.Now().UTC(), StatusIntervalSeconds: int(interval.Seconds()),
		HeartbeatTimeoutSeconds: int(timeout.Seconds()), ManagedState: managedState(ready), RoutingReady: ready,
	})
	if err := writeV2Envelope(ctx, conn, welcome); err != nil {
		return
	}
	if s.Logger != nil {
		s.Logger.Info(ctx, "control.v2 WebSocket 会话已建立",
			slog.String("node_id", session.NodeID), slog.String("session_id", session.ID),
			slog.String("software_version", hello.SoftwareVersion))
	}

	queue := controlv2.NewQueue(512, 4<<20)
	s.queues.Store(session.NodeID, queue)
	defer func() { s.queues.CompareAndDelete(session.NodeID, queue); queue.Close() }()
	if err := s.serveV2Session(ctx, conn, session, queue); err != nil {
		closeReason = err.Error()
		if s.Logger != nil {
			s.Logger.Warn(ctx, "control.v2 会话结束", slog.String("node_id", session.NodeID),
				slog.String("session_id", session.ID), slog.String("error", closeReason))
		}
	}
}

func (s *V2Server) isCurrentV2Connection(nodeID string, conn *websocket.Conn) bool {
	current, ok := s.registry.Load(nodeID)
	return ok && current == conn
}

func (s *V2Server) handleV2Message(ctx context.Context, session Session, queue *controlv2.Queue, envelope protocolv2.Envelope) error {
	switch envelope.Type {
	case protocolv2.TypeNodeStatus:
		status, err := protocolv2.Decode[protocolv2.NodeStatus](envelope)
		if err != nil {
			return err
		}
		return s.Repo.AcceptV2NodeStatus(ctx, session, status)
	default:
		return s.handleV2BusinessMessage(ctx, session, queue, envelope)
	}
}

func (s *V2Server) writeDirectError(ctx context.Context, conn *websocket.Conn, code, message string) {
	e, _ := protocolv2.New(protocolv2.TypeProtocolError, mustID(), protocolv2.ProtocolError{Code: code, Message: message})
	_ = writeV2Envelope(ctx, conn, e)
}

func readV2Envelope(ctx context.Context, conn *websocket.Conn) (protocolv2.Envelope, error) {
	messageType, data, err := conn.Read(ctx)
	if err != nil {
		return protocolv2.Envelope{}, err
	}
	if messageType != websocket.MessageText {
		return protocolv2.Envelope{}, errors.New("control.v2 requires text JSON messages")
	}
	var envelope protocolv2.Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return protocolv2.Envelope{}, err
	}
	if err := envelope.Validate(); err != nil {
		return protocolv2.Envelope{}, err
	}
	return envelope, nil
}

func writeV2Envelope(ctx context.Context, conn *websocket.Conn, envelope protocolv2.Envelope) error {
	data, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	if len(data) > protocolv2.MaxMessageBytes {
		return errors.New("control.v2 message exceeds hard limit")
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
}

func runV2Writer(ctx context.Context, conn *websocket.Conn, queue *controlv2.Queue) error {
	for {
		envelope, err := queue.Dequeue(ctx)
		if err != nil {
			return err
		}
		if err := writeV2Envelope(ctx, conn, envelope); err != nil {
			return err
		}
		queue.ReplaySent(envelope.ID)
	}
}
