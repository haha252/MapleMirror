package syncer

import (
	"errors"
	"strconv"
	"strings"
)

func validateSwarmContentRange(value string, start, end, total int64) error {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "bytes ") {
		return errors.New("range response missing valid Content-Range")
	}
	rangeAndTotal := strings.TrimPrefix(value, "bytes ")
	rangePart, totalPart, ok := strings.Cut(rangeAndTotal, "/")
	if !ok || totalPart == "*" {
		return errors.New("range response Content-Range total is invalid")
	}
	firstPart, lastPart, ok := strings.Cut(rangePart, "-")
	if !ok {
		return errors.New("range response Content-Range bounds are invalid")
	}
	first, err := strconv.ParseInt(firstPart, 10, 64)
	if err != nil {
		return errors.New("range response Content-Range start is invalid")
	}
	last, err := strconv.ParseInt(lastPart, 10, 64)
	if err != nil {
		return errors.New("range response Content-Range end is invalid")
	}
	gotTotal, err := strconv.ParseInt(totalPart, 10, 64)
	if err != nil {
		return errors.New("range response Content-Range total is invalid")
	}
	if end <= start || first != start || last != end-1 || gotTotal != total {
		return errors.New("range response Content-Range does not match request")
	}
	return nil
}
