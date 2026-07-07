package config

type AbuseControl struct {
	Mode       string         `yaml:"mode"`
	Challenge  AbuseChallenge  `yaml:"challenge"`
	Blocked    AbuseBlocked    `yaml:"blocked"`
	Punishment AbusePunishment `yaml:"punishment"`
}

type AbuseChallenge struct {
	BurstWindow          string               `yaml:"burst_window"`
	RollingWindow        string               `yaml:"rolling_window"`
	InvalidSolutionWeight int                  `yaml:"invalid_solution_weight"`
	ElevatedBits         int                  `yaml:"elevated_bits"`
	SevereBits           int                  `yaml:"severe_bits"`
	MaxBits              int                  `yaml:"max_bits"`
	Exact                AbuseThresholdWindow `yaml:"exact"`
	Network              AbuseThresholdWindow `yaml:"network"`
}

type AbuseThresholdWindow struct {
	ElevatedBurst   int `yaml:"elevated_burst"`
	ElevatedRolling int `yaml:"elevated_rolling"`
	SevereBurst     int `yaml:"severe_burst"`
	SevereRolling   int `yaml:"severe_rolling"`
	RejectBurst     int `yaml:"reject_burst"`
	RejectRolling   int `yaml:"reject_rolling"`
}

type AbuseBlocked struct {
	FlushInterval string           `yaml:"flush_interval"`
	FlushBatch    int              `yaml:"flush_batch"`
	CacheNegative string           `yaml:"cache_negative_ttl"`
	Escalation    AbuseEscalation  `yaml:"escalation"`
}

type AbuseEscalation struct {
	Level1Attempts int    `yaml:"level_1_attempts"`
	Level1Duration  string `yaml:"level_1_duration"`
	Level2Attempts  int    `yaml:"level_2_attempts"`
	Level2Duration  string `yaml:"level_2_duration"`
	Level3Attempts  int    `yaml:"level_3_attempts"`
	Level3Duration  string `yaml:"level_3_duration"`
}

type AbusePunishment struct {
	Enabled        *bool  `yaml:"enabled"`
	Difficulty     int    `yaml:"difficulty"`
	TotalAttempts  int    `yaml:"total_attempts"`
	BurstAttempts  int    `yaml:"burst_attempts"`
	RollingAttempts int   `yaml:"rolling_attempts"`
	WorkerLimit    int    `yaml:"worker_limit"`
}
