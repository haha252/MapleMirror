package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const (
	MaxFrameBytes     = 1 << 20
	MaxHeartbeatBytes = 16 << 10
	MaxPressureBytes  = 16 << 10
)

func ReadFrame(r io.Reader, limit uint32) (Envelope, error) {
	if limit == 0 || limit > MaxFrameBytes {
		limit = MaxFrameBytes
	}
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Envelope{}, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > limit {
		return Envelope{}, errors.New("控制消息帧长度超出限制")
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return Envelope{}, err
	}
	if !utf8.Valid(body) {
		return Envelope{}, errors.New("控制消息不是有效 UTF-8")
	}
	var envelope Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Envelope{}, fmt.Errorf("控制消息 JSON 无效：%w", err)
	}
	return envelope, nil
}

func WriteFrame(w io.Writer, envelope Envelope) error {
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("编码控制消息失败：%w", err)
	}
	if len(body) == 0 || len(body) > MaxFrameBytes {
		return errors.New("控制消息帧长度超出限制")
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(body)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
