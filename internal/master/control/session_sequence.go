package control

import "fmt"

func (r Repository) currentSequence(session Session) (uint64, error) {
	last, err := r.runtime().CurrentSequence(session)
	if err != nil {
		return 0, fmt.Errorf("%w", ErrSessionUnavailable)
	}
	return last, nil
}

func (r Repository) updateSequence(session Session, seq uint64) error {
	if err := r.runtime().UpdateSequence(session, seq); err != nil {
		return fmt.Errorf("%w", ErrSessionUnavailable)
	}
	return nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
