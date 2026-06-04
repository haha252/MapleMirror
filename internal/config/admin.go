package config

type Administration struct {
	AllowedCIDRs        []string `yaml:"allowed_cidrs"`
	TokenEnv            string   `yaml:"token_env"`
	TokenFile           string   `yaml:"token_file"`
	TokenMinBytes       int      `yaml:"token_min_bytes"`
	HighRiskRequireMTLS *bool    `yaml:"high_risk_require_mtls"`
	Web                 AdminWeb `yaml:"web"`
	TLS                 AdminTLS `yaml:"tls"`
}

type AdminWeb struct {
	Enabled              *bool  `yaml:"enabled"`
	UsersFile            string `yaml:"users_file"`
	BootstrapPasswordEnv string `yaml:"bootstrap_password_env"`
	SessionSecretFile    string `yaml:"session_secret_file"`
	SessionTTL           string `yaml:"session_ttl"`
	LoginFailureWindow   string `yaml:"login_failure_window"`
	LoginFailureLimit    int    `yaml:"login_failure_limit"`
	LoginBanDuration     string `yaml:"login_ban_duration"`
}

type AdminTLS struct {
	CertFile     string `yaml:"cert_file"`
	KeyFile      string `yaml:"key_file"`
	ClientCAFile string `yaml:"client_ca_file"`
}
