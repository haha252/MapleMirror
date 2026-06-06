package adminui

import mastercontrol "mirror-server/internal/master/control"

func (s *Server) pairingRequestsResponse(items []mastercontrol.Enrollment) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id": item.ID, "public_name": item.PublicName,
			"fingerprint": item.Fingerprint, "status": item.Status,
			"expires_at": item.ExpiresAt.In(s.location()).Format(adminTimeLayout),
			"request_id": item.RequestID, "capabilities": item.Capabilities,
		})
	}
	return out
}
