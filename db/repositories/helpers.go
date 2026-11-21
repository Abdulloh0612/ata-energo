package repositories

import (
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

// rowScanner — общий интерфейс pgx.Row и pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// notFound превращает pgx.ErrNoRows в (nil, nil), как в остальных репозиториях проекта.
func notFound[T any](v *T, err error) (*T, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

// jsonMap — JSONB-колонки NOT NULL: nil-карту пишем как {}.
func jsonMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// jsonList — nil-список пишем как [].
func jsonList(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// like — шаблон для ILIKE с экранированием спецсимволов.
func like(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(s) + "%"
}

// collect читает все строки через функцию scan.
func collect[T any](rows pgx.Rows, scan func(rowScanner) (T, error)) ([]T, error) {
	defer rows.Close()
	out := make([]T, 0)
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
