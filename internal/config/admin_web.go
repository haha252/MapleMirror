package config

import "errors"

func applyAdminWebDefaults(c *Master, warn WarnFunc) {
	defaultBool(&c.Admin.Web.Enabled, false, "admin.web.enabled", warn)
	defaultBool(&c.Admin.Web.HTTPSEnabled, true, "admin.web.https_enabled", warn)
	defaultBool(&c.Admin.Web.ExclusiveAPI, false, "admin.web.exclusive_api", warn)
	defaultBool(&c.Admin.Web.HighRiskSessionAllowed, false, "admin.web.high_risk_session_allowed", warn)
	setString(&c.Admin.Web.UsersFile, "secrets/admin-users.yaml", "admin.web.users_file", warn)
	setString(&c.Admin.Web.BootstrapPasswordEnv, "MIRROR_ADMIN_WEB_PASSWORD", "admin.web.bootstrap_password_env", warn)
	setString(&c.Admin.Web.SessionSecretFile, "secrets/admin-web-session.key", "admin.web.session_secret_file", warn)
	setString(&c.Admin.Web.SessionTTL, "12h", "admin.web.session_ttl", warn)
	setString(&c.Admin.Web.LoginFailureWindow, "24h", "admin.web.login_failure_window", warn)
	if c.Admin.Web.LoginFailureLimit == 0 {
		c.Admin.Web.LoginFailureLimit = 3
		warnDefault(warn, "admin.web.login_failure_limit", "3")
	}
	setString(&c.Admin.Web.LoginBanDuration, "168h", "admin.web.login_ban_duration", warn)
}

func defaultBool(value **bool, fallback bool, field string, warn WarnFunc) {
	if *value == nil {
		selected := fallback
		*value = &selected
		warnDefault(warn, field, boolText(fallback))
	}
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func validateAdminWeb(web AdminWeb) error {
	if web.Enabled == nil || !*web.Enabled {
		return nil
	}
	if web.UsersFile == "" || web.SessionSecretFile == "" {
		return errors.New("管理面板启用时必须配置用户文件和会话密钥文件")
	}
	if web.LoginFailureLimit < 1 {
		return errors.New("管理面板登录失败封禁阈值必须大于零")
	}
	if web.HighRiskSessionAllowed != nil && *web.HighRiskSessionAllowed &&
		(web.ExclusiveAPI == nil || !*web.ExclusiveAPI) {
		return errors.New("admin.web.high_risk_session_allowed 只能在 admin.web.exclusive_api=true 时启用")
	}
	if web.HTTPSEnabled != nil && !*web.HTTPSEnabled &&
		(web.ExclusiveAPI == nil || !*web.ExclusiveAPI ||
			web.HighRiskSessionAllowed == nil || !*web.HighRiskSessionAllowed) {
		return errors.New("admin.web.https_enabled=false 只能在 exclusive_api=true 且 high_risk_session_allowed=true 时启用")
	}
	return nil
}
