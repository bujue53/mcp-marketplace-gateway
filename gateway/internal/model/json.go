package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
)

// JSON 使任意 JSON 结构可在 database/sql 与 pgx 中作为 jsonb 存取。
type JSON map[string]any

func (j JSON) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	b, err := json.Marshal(j)
	return string(b), err
}

func (j *JSON) Scan(value any) error {
	if value == nil {
		*j = nil
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return errors.New("unsupported JSON scan type")
	}
	return json.Unmarshal(b, j)
}

