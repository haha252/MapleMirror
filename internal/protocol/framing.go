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
		return Envelope{}, fmt.Errorf("控制消息帧长度超出限制: size=%d limit=%d", size, limit)
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

// FrameReader preserves partially read frame bytes across temporary read
// errors such as socket deadlines. Callers that poll a stream with short
// deadlines must reuse one FrameReader for the lifetime of the connection.
type FrameReader struct {
	limit      uint32
	header     [4]byte
	headerRead int
	body       []byte
	bodyRead   int
}

func NewFrameReader(limit uint32) *FrameReader {
	if limit == 0 || limit > MaxFrameBytes {
		limit = MaxFrameBytes
	}
	return &FrameReader{limit: limit}
}

func (r *FrameReader) ReadFrame(source io.Reader) (Envelope, error) {
	if r.limit == 0 || r.limit > MaxFrameBytes {
		r.limit = MaxFrameBytes
	}
	if r.headerRead < len(r.header) {
		n, err := io.ReadFull(source, r.header[r.headerRead:])
		r.headerRead += n
		if err != nil {
			return Envelope{}, err
		}
	}
	if r.body == nil {
		size := binary.BigEndian.Uint32(r.header[:])
		if size == 0 || size > r.limit {
			return Envelope{}, fmt.Errorf("控制消息帧长度超出限制: size=%d limit=%d", size, r.limit)
		}
		r.body = make([]byte, size)
	}
	if r.bodyRead < len(r.body) {
		n, err := io.ReadFull(source, r.body[r.bodyRead:])
		r.bodyRead += n
		if err != nil {
			return Envelope{}, err
		}
	}
	body := r.body
	r.reset()
	return decodeFrameBody(body)
}

func (r *FrameReader) reset() {
	r.header = [4]byte{}
	r.headerRead = 0
	r.body = nil
	r.bodyRead = 0
}

func decodeFrameBody(body []byte) (Envelope, error) {
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
