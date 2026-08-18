package control

import (
	"net"
	"time"
)

const controlIdleRead = 200 * time.Millisecond
const controlWakePoll = 10 * time.Millisecond
const trafficReplayBurstLimit = 20

type sessionLoopState struct {
	sequence           uint64
	nextHeartbeat      time.Time
	pendingInventory   *pendingInventoryReport
	trafficReplayBurst int
}

func (c *Client) sendSessionReports(conn net.Conn, reqID string,
	interval time.Duration) error {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	c.HeartbeatInterval = interval
	if c.controlWorkWake == nil {
		c.controlWorkWake = make(chan struct{}, 1)
	}
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
		next, handled, err := c.waitForControlEventOrTask(conn, reqID,
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

func (c *Client) sendHeartbeatWindow(conn net.Conn, reqID string,
	state *sessionLoopState, interval time.Duration) error {
	actualBandwidth := c.sampleBandwidth()
	next, err := c.heartbeat(conn, reqID, state.sequence, actualBandwidth)
	if err != nil {
		return err
	}
	state.sequence = next
	next, _, err = c.flushPendingPublicProbeReady(conn, reqID, state.sequence, true)
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

func (c Client) waitForControlEventOrTask(conn net.Conn, reqID string,
	sequence uint64, wait time.Duration) (uint64, bool, error) {
	if c.controlWorkWake == nil {
		return c.readOptionalTaskWithTimeout(conn, reqID, sequence, wait)
	}
	deadline := time.Now().Add(wait)
	for {
		select {
		case <-c.controlWorkWake:
			return sequence, false, nil
		default:
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return sequence, false, nil
		}
		timeout := controlWakePoll
		if remaining < timeout {
			timeout = remaining
		}
		next, handled, err := c.readOptionalTaskWithTimeout(conn, reqID, sequence, timeout)
		if err != nil || handled {
			return next, handled, err
		}
	}
}

func (c Client) wakeControlWork() {
	if c.controlWorkWake == nil {
		return
	}
	select {
	case c.controlWorkWake <- struct{}{}:
	default:
	}
}

func (c *Client) sendNextControlWork(conn net.Conn, reqID string,
	state *sessionLoopState) (bool, error) {
	next, sent, err := c.flushPendingPublicProbeReady(conn, reqID, state.sequence, false)
	if err != nil || sent {
		state.sequence = next
		return sent, err
	}
	next, err = c.sendPendingTaskResults(conn, reqID, state.sequence)
	sent = next != state.sequence
	if err != nil || sent {
		state.sequence = next
		return sent, err
	}
	next, sent, err = c.sendNextRunningTaskAck(conn, reqID, state.sequence)
	if err != nil || sent {
		state.sequence = next
		return sent, err
	}
	if state.trafficReplayBurst < trafficReplayBurstLimit {
		next, sent, err = c.sendNextTrafficEvent(conn, reqID, state.sequence)
		if err != nil || sent {
			state.sequence = next
			if sent {
				state.trafficReplayBurst++
			}
			return sent, err
		}
		state.trafficReplayBurst = 0
	}
	next, sent, err = c.sendNextInventoryReportChunk(conn, reqID, state.sequence,
		&state.pendingInventory)
	if err != nil || sent {
		state.sequence = next
		if sent {
			state.trafficReplayBurst = 0
		}
		return sent, err
	}
	next, sent, err = c.sendNextAuthorizationStatusEvent(conn, reqID, state.sequence)
	if err != nil || sent {
		state.sequence = next
		if sent {
			state.trafficReplayBurst = 0
		}
		return sent, err
	}
	if state.trafficReplayBurst >= trafficReplayBurstLimit {
		state.trafficReplayBurst = 0
		next, sent, err = c.sendNextTrafficEvent(conn, reqID, state.sequence)
		if sent {
			state.trafficReplayBurst = 1
		}
	}
	state.sequence = next
	return sent, err
}
