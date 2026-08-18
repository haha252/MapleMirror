package control

import (
	"fmt"
	"net"
	"sync"
	"testing"

	"mirror-server/internal/protocol"
)

func TestWriteControlFrameSerializesConcurrentFrames(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	const count = 20
	readDone := make(chan error, 1)
	go func() {
		for i := 0; i < count; i++ {
			if _, err := protocol.ReadFrame(clientConn, protocol.MaxFrameBytes); err != nil {
				readDone <- err
				return
			}
		}
		readDone <- nil
	}()

	conn := &serializedConn{Conn: serverConn}
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := writeControlFrame(conn, protocol.Envelope{
				ProtocolVersion: protocol.Version, MessageID: fmt.Sprintf("msg-%d", i),
				MessageType: protocol.TypeHeartbeat, NodeID: "node-1",
				RequestID: "req-1", Sequence: uint64(i + 1),
				Payload: []byte(`{"status":"ok"}`),
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if err := <-readDone; err != nil {
		t.Fatal(err)
	}
}
