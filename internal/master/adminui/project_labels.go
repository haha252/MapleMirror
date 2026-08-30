package adminui

import "context"

func (s *Server) projectNames(ctx context.Context) map[string]string {
	names := map[string]string{}
	rows, err := s.repo.DB.QueryContext(ctx, "SELECT id, name FROM projects")
	if err != nil {
		return names
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) == nil {
			names[id] = name
		}
	}
	return names
}
