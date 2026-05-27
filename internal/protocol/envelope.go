package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Envelope struct {
	ProtocolVersion string          `json:"protocol_version"`
	MessageID       string          `json:"message_id"`
	MessageType     string          `json:"message_type"`
	SentAt          time.Time       `json:"sent_at"`
	NodeID          string          `json:"node_id"`
	RequestID       string          `json:"request_id"`
	ReplyTo         string          `json:"reply_to,omitempty"`
	Sequence        uint64          `json:"sequence"`
	Payload         json.RawMessage `json:"payload"`
}

type Channel int

const (
	Enrollment Channel = iota + 1
	Control
)

func (e Envelope) Validate(channel Channel) error {
	if e.ProtocolVersion != Version {
		return errors.New("不支持的控制协议版本")
	}
	if e.MessageID == "" || e.MessageType == "" || e.RequestID == "" {
		return errors.New("控制消息缺少必要标识")
	}
	if e.SentAt.IsZero() {
		return errors.New("控制消息缺少发送时间")
	}
	if len(e.Payload) == 0 {
		return errors.New("控制消息缺少载荷")
	}
	switch channel {
	case Enrollment:
		if !AllowedOnEnrollment(e.MessageType) {
			return fmt.Errorf("登记通道不允许消息类型 %s", e.MessageType)
		}
	case Control:
		if e.NodeID == "" {
			return errors.New("控制通道消息必须携带节点标识")
		}
		if !AllowedOnControl(e.MessageType) {
			return fmt.Errorf("控制通道不允许消息类型 %s", e.MessageType)
		}
	default:
		return errors.New("未知控制通道")
	}
	return nil
}
