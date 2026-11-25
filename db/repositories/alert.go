package repositories

import (
	"context"
	"fmt"
	"strings"

	"ata-energo/types"

	"github.com/jackc/pgx/v5/pgxpool"
)

type AlertRepo struct {
	pool *pgxpool.Pool
}

func NewAlertRepo(pool *pgxpool.Pool) *AlertRepo {
	return &AlertRepo{pool: pool}
}

const alertSelect = `SELECT a.id, a.device_id, d.device_id, d.device_type, d.client_id, c.name, a.kind, a.text,
	a.created_at, a.is_read, a.response_status, a.response_user_id
	FROM alerts a JOIN devices d ON d.id = a.device_id JOIN clients c ON c.id = d.client_id`

func scanAlert(row rowScanner) (types.Alert, error) {
	var a types.Alert
	err := row.Scan(&a.ID, &a.DeviceRef, &a.DeviceCode, &a.DeviceType, &a.ClientID, &a.ClientName, &a.Kind, &a.Text,
		&a.CreatedAt, &a.IsRead, &a.ResponseStatus, &a.ResponseUserID)
	return a, err
}

func (r *AlertRepo) Create(ctx context.Context, deviceID int64, kind, text string) (*types.Alert, error) {
	if len([]rune(text)) > 500 {
		text = string([]rune(text)[:500])
	}
	var id int64
	if err := r.pool.QueryRow(ctx, `INSERT INTO alerts (device_id, kind, text) VALUES ($1,$2,$3) RETURNING id`,
		deviceID, kind, text).Scan(&id); err != nil {
		return nil, err
	}
	return r.GetByID(ctx, id, nil)
}

func (r *AlertRepo) GetByID(ctx context.Context, id int64, ownerUserID *int64) (*types.Alert, error) {
	q := alertSelect + ` WHERE a.id = $1`
	args := []any{id}
	if ownerUserID != nil {
		q += ` AND c.user_id = $2`
		args = append(args, *ownerUserID)
	}
	a, err := scanAlert(r.pool.QueryRow(ctx, q, args...))
	return notFound(&a, err)
}

// List — аварии: сотрудники видят все, клиент — только свои (OwnerUserID). ?unread=1&client=ID
func (r *AlertRepo) List(ctx context.Context, f types.AlertFilter) ([]types.Alert, error) {
	var where []string
	var args []any
	if f.OwnerUserID != nil {
		args = append(args, *f.OwnerUserID)
		where = append(where, fmt.Sprintf("c.user_id = $%d", len(args)))
	}
	if f.Unread {
		where = append(where, "a.is_read = FALSE")
	}
	if f.ClientID != nil {
		args = append(args, *f.ClientID)
		where = append(where, fmt.Sprintf("d.client_id = $%d", len(args)))
	}
	q := alertSelect
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 200
	}
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, q+fmt.Sprintf(` ORDER BY a.created_at DESC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanAlert)
}

func (r *AlertRepo) MarkRead(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE alerts SET is_read = TRUE WHERE id = $1`, id)
	return err
}

func (r *AlertRepo) MarkAllRead(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE alerts SET is_read = TRUE WHERE is_read = FALSE`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
