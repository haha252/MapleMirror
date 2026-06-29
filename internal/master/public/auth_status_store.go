package public

import "context"

func (s Store) Authorization(ctx context.Context, id string) (AuthorizationStatus, error) {
	var out AuthorizationStatus
	err := s.DB.QueryRowContext(ctx, `SELECT da.id, da.asset_id, da.node_id,
		COALESCE(NULLIF(n.public_name, ''), '节点不可用'), da.client_prefix_key,
		da.status, da.expires_at, da.token_hash FROM download_authorizations da
		LEFT JOIN nodes n ON n.id = da.node_id WHERE da.id = ?`, id).
		Scan(&out.AuthorizationID, &out.AssetID, &out.NodeID, &out.NodeName,
			&out.ClientPrefixKey, &out.State, &out.ExpiresAt, &out.TokenHash)
	return out, err
}
