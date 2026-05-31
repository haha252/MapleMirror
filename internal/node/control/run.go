package control

import (
	"context"
	"log/slog"
	"time"
)

func (c *Client) Run(stop <-chan struct{}) {
	interval := c.HeartbeatInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		if c.Logger != nil {
			c.Logger.Debug(context.Background(), "节点控制轮询开始",
				slog.String("node_id", c.NodeID),
				slog.String("master", c.Address),
				slog.String("interval", interval.String()))
		}
		started := time.Now()
		nextInterval, err := c.RunOnce()
		if err != nil {
			if c.Logger != nil {
				c.Logger.Warn(context.Background(), "节点控制轮询失败",
					slog.String("node_id", c.NodeID),
					slog.String("master", c.Address),
					slog.String("error", err.Error()))
			}
		} else if nextInterval > 0 {
			interval = nextInterval
			c.HeartbeatInterval = nextInterval
			if c.Logger != nil {
				c.Logger.Debug(context.Background(), "节点控制轮询完成",
					slog.String("node_id", c.NodeID),
					slog.String("master", c.Address),
					slog.String("next_interval", nextInterval.String()))
			}
		}
		select {
		case <-stop:
			return
		case <-time.After(nextDelay(interval, time.Since(started))):
		}
	}
}

func nextDelay(interval, elapsed time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	if elapsed >= interval {
		return 0
	}
	return interval - elapsed
}
