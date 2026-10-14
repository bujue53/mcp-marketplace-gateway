package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func validJournalText(value string) string { return strings.ToValidUTF8(value, "\uFFFD") }

// 持有被引用实体的键锁；已删除的实体记为 NULL，避免一条引用失效阻塞整批日志。
func journalIDs(ctx context.Context, tx pgx.Tx, table string, ids []int64) (map[int64]bool, error) {
	if table != "users" && table != "mcp_tools" {
		return nil, fmt.Errorf("invalid journal reference table")
	}
	rows, err := tx.Query(ctx, "SELECT id FROM "+table+" WHERE id=ANY($1::bigint[]) FOR KEY SHARE", ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		found[id] = true
	}
	return found, rows.Err()
}
