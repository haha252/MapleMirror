package public

import "time"

func (l TokenLifetime) normalized() TokenLifetime {
	if l.FirstConnectionTimeout <= 0 {
		l.FirstConnectionTimeout = 20 * time.Second
	}
	if l.IdleTimeout <= 0 {
		l.IdleTimeout = 120 * time.Second
	}
	if l.MaxDuration <= 0 {
		l.MaxDuration = 30 * time.Minute
	}
	return l
}

func durationSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(d / time.Second)
}
