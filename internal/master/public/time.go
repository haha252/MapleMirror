package public

import "time"

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
