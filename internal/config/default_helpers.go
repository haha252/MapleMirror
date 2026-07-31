package config

func setString(value *string, fallback, field string, warn WarnFunc) {
	if *value == "" {
		*value = fallback
		warnDefault(warn, field, fallback)
	}
}
