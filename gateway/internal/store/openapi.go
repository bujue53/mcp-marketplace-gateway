package store

import (
	"context"
	"encoding/json"
)

func (s *Store) SaveOpenAPIBindings(ctx context.Context, serverID int64, bindings json.RawMessage) error {
	_, err := s.P.Exec(ctx, `INSERT INTO openapi_bindings(server_id,bindings) VALUES($1,$2)
 ON CONFLICT(server_id) DO UPDATE SET bindings=EXCLUDED.bindings, updated_at=now()`, serverID, bindings)
	return err
}
func (s *Store) LoadOpenAPIBindings(ctx context.Context, serverID int64) (json.RawMessage, error) {
	var bindings json.RawMessage
	err := s.P.QueryRow(ctx, `SELECT bindings FROM openapi_bindings WHERE server_id=$1`, serverID).Scan(&bindings)
	return bindings, err
}
