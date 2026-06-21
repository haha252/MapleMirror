package config

func masterDurations(c Master) map[string]string {
	return map[string]string{
		"database.busy_timeout":                   c.Database.BusyTimeout,
		"scan.interval":                           c.Scan.Interval,
		"altcha.challenge_ttl":                    c.ALTCHA.ChallengeTTL,
		"api_pow.challenge_ttl":                   c.APIPoW.ChallengeTTL,
		"download_token.first_connection_timeout": c.DownloadToken.FirstConnectionTimeout,
		"download_token.idle_timeout":             c.DownloadToken.IdleTimeout,
		"download_token.max_duration":             c.DownloadToken.MaxDuration,
		"node.heartbeat_timeout":                  c.Node.HeartbeatTimeout,
		"node.heartbeat_offline_grace":            c.Node.HeartbeatOfflineGrace,
		"node.heartbeat_interval":                 c.Node.HeartbeatInterval,
		"node.public_probe_interval":              c.Node.PublicProbeInterval,
		"node.public_probe_timeout":               c.Node.PublicProbeTimeout,
		"node.public_probe_ttl":                   c.Node.PublicProbeTTL,
		"node.enrollment_timeout":                 c.Node.EnrollmentTimeout,
		"node.pairing_code_ttl":                   c.Node.PairingCodeTTL,
		"admin.web.session_ttl":                   c.Admin.Web.SessionTTL,
		"admin.web.login_failure_window":          c.Admin.Web.LoginFailureWindow,
		"admin.web.login_ban_duration":            c.Admin.Web.LoginBanDuration,
	}
}
