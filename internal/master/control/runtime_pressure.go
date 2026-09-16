package control

import "time"

type RoutingPressure struct {
	PressureRatio   float64
	ActiveDownloads int64
	ReportedAt      time.Time
	Valid           bool
}

func (s *RuntimeStore) LatestRoutingPressure(nodeID string, maxAge time.Duration) RoutingPressure {
	if s == nil {
		return RoutingPressure{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	node := s.latest[nodeID]
	out := newerRoutingPressure(node.Heartbeat, node.Pressure)
	if v2 := routingPressureFromV2(node.V2Status); v2.Valid && (!out.Valid || v2.ReportedAt.After(out.ReportedAt)) {
		out = v2
	}
	if !out.Valid || maxAge <= 0 {
		return out
	}
	if time.Since(out.ReportedAt) > maxAge {
		return RoutingPressure{}
	}
	return out
}

func newerRoutingPressure(hb runtimeHeartbeat, report runtimePressureReport) RoutingPressure {
	hbPressure := routingPressureFromHeartbeat(hb)
	reportPressure := routingPressureFromReport(report)
	if !hbPressure.Valid {
		return reportPressure
	}
	if !reportPressure.Valid || hbPressure.ReportedAt.After(reportPressure.ReportedAt) {
		return hbPressure
	}
	return reportPressure
}

func routingPressureFromHeartbeat(hb runtimeHeartbeat) RoutingPressure {
	if !hb.Valid {
		return RoutingPressure{}
	}
	reported, err := time.Parse(time.RFC3339Nano, hb.ReportedAt)
	if err != nil {
		return RoutingPressure{}
	}
	return RoutingPressure{
		PressureRatio:   hb.PressureRatio,
		ActiveDownloads: hb.ActiveDownloads,
		ReportedAt:      reported,
		Valid:           true,
	}
}

func routingPressureFromReport(report runtimePressureReport) RoutingPressure {
	if !report.Valid {
		return RoutingPressure{}
	}
	reported, err := time.Parse(time.RFC3339Nano, report.ReportedAt)
	if err != nil {
		return RoutingPressure{}
	}
	return RoutingPressure{
		PressureRatio:   report.PressureRatio,
		ActiveDownloads: report.ActiveDownloads,
		ReportedAt:      reported,
		Valid:           true,
	}
}

func routingPressureFromV2(item runtimeV2Status) RoutingPressure {
	if item.ReportedAt.IsZero() {
		return RoutingPressure{}
	}
	ratio := float64(0)
	if item.Status.TargetBandwidthBPS > 0 {
		ratio = float64(item.Status.ActualBandwidthBPS) / float64(item.Status.TargetBandwidthBPS)
	}
	return RoutingPressure{PressureRatio: ratio, ActiveDownloads: item.Status.PublicActiveDownloads,
		ReportedAt: item.ReportedAt, Valid: true}
}
