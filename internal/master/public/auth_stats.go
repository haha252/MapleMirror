package public

func (s Store) bufferAuthorizationStats(day, assetID, projectID string, webAuth, apiAuth int64) {
	if s.StatsBuffer == nil {
		return
	}
	counters := statCounter(0, 1, webAuth, apiAuth, 0, 0)
	s.StatsBuffer.AddPublic(day, counters)
	s.StatsBuffer.AddAsset(assetID, counters)
	s.StatsBuffer.AddProject(projectID, counters)
}

func trafficLimitBytes(limit int64, exempt bool) int64 {
	if exempt {
		return 0
	}
	return limit
}
