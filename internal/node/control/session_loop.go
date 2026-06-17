package control

import (
	"net"
	"time"
)

const controlIdleRead = 200 * time.Millisecond

type sessionLoopState struct {
	sequence         uint64
	nextHeartbeat    time.Time
	pendingInventory *pendingInventoryReport
}

func (c Client) sendSessionReports(conn net.Conn, reqID string,
	interval time.Duration) error {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	c.HeartbeatInterval = interval
	state := sessionLoopState{
		sequence:      2,
		nextHeartbeat: time.Now(),
	}
	for {
		if !time.Now().Before(state.nextHeartbeat) {
			if err := c.sendHeartbeatWindow(conn, reqID, &state, interval); err != nil {
				return err
			}
			continue
		}
		sent, err := c.sendNextControlWork(conn, reqID, &state)
		if err != nil {
			return err
		}
		if sent {
			continue
		}
		wait := time.Until(state.nextHeartbeat)
		if wait > controlIdleRead {
			wait = controlIdleRead
		}
		if wait <= 0 {
			continue
		}
		next, handled, err := c.readOptionalTaskWithTimeout(conn, reqID,
			state.sequence, wait)
		state.sequence = next
		if err != nil {
			return err
		}
		if !handled {
			continue
		}
	}
}

func (c Client) sendHeartbeatWindow(conn net.Conn, reqID string,
	state *sessionLoopState, interval time.Duration) error {
	actualBandwidth := c.sampleBandwidth()
	next, err := c.heartbeat(conn, reqID, state.sequence, actualBandwidth)
	if err != nil {
		return err
	}
	state.sequence = next
	next, err = c.sendPressureReport(conn, reqID, state.sequence, actualBandwidth)
	if err != nil {
		return err
	}
	state.sequence = next
	next, err = c.readOptionalTasksToCapacity(conn, reqID, state.sequence)
	if err != nil {
		return err
	}
	state.sequence = next
	state.nextHeartbeat = time.Now().Add(interval)
	return nil
}

func (c Client) sendNextControlWork(conn net.Conn, reqID string,
	state *sessionLoopState) (bool, error) {
	next, sent, err := c.sendNextPendingTaskResult(conn, reqID, state.sequence)
	if err != nil || sent {
		state.sequence = next
		return sent, err
	}
	next, sent, err = c.sendNextRunningTaskAck(conn, reqID, state.sequence)
	if err != nil || sent {
		state.sequence = next
		return sent, err
	}
	next, sent, err = c.sendNextInventoryReportChunk(conn, reqID, state.sequence,
		&state.pendingInventory)
	if err != nil || sent {
		state.sequence = next
		return sent, err
	}
	next, sent, err = c.sendNextTrafficEvent(conn, reqID, state.sequence)
	state.sequence = next
	return sent, err
}
