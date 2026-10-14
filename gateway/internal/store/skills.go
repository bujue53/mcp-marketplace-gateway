package store

import (
	"context"
	"encoding/json"
)

func (s *Store) SaveSkillsBindings(ctx context.Context, id int64, skills json.RawMessage) error {
	_, err := s.P.Exec(ctx, `INSERT INTO skills_bindings(server_id,skills) VALUES($1,$2) ON CONFLICT(server_id) DO UPDATE SET skills=EXCLUDED.skills,updated_at=now()`, id, skills)
	return err
}
func (s *Store) LoadSkillsBindings(ctx context.Context, id int64) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.P.QueryRow(ctx, `SELECT skills FROM skills_bindings WHERE server_id=$1`, id).Scan(&raw)
	return raw, err
}
