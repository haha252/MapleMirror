package files

func (h *Handler) enter(id string, limit int) bool {
	if limit <= 0 {
		limit = 1
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active == nil {
		h.active = make(map[string]int)
	}
	if h.active[id] >= limit {
		return false
	}
	h.active[id]++
	return true
}

func (h *Handler) leave(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active[id] <= 1 {
		delete(h.active, id)
		if !h.hasPendingTrafficLocked(id) {
			delete(h.budgets, id)
		}
		return
	}
	h.active[id]--
}
