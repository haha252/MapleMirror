package eventwake

// Notifier coalesces wakeups. Durable state remains in SQLite; the channel only
// tells the control loop that it should query for newly pending work.
type Notifier struct{ ch chan struct{} }

func New() *Notifier { return &Notifier{ch: make(chan struct{}, 1)} }
func (n *Notifier) Wake() {
	if n == nil {
		return
	}
	select {
	case n.ch <- struct{}{}:
	default:
	}
}
func (n *Notifier) C() <-chan struct{} {
	if n == nil {
		return nil
	}
	return n.ch
}
