package repositories

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ata-energo/types"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ModemRepo struct {
	pool *pgxpool.Pool
}

func NewModemRepo(pool *pgxpool.Pool) *ModemRepo {
	return &ModemRepo{pool: pool}
}

const modemCols = `m.id, m.client_id, m.serial, m.name, m.has_generator, m.site, m.agent_id, m.connection,
	m.sections, m.status, m.last_seen, m.created_at, c.name`

const modemSelect = `SELECT ` + modemCols + ` FROM modems m JOIN clients c ON c.id = m.client_id`

func scanModem(row rowScanner) (types.Modem, error) {
	var m types.Modem
	err := row.Scan(&m.ID, &m.ClientID, &m.Serial, &m.Name, &m.HasGenerator, &m.Site, &m.AgentID, &m.Connection,
		&m.Sections, &m.Status, &m.LastSeen, &m.CreatedAt, &m.ClientName)
	if m.Connection == nil {
		m.Connection = map[string]any{}
	}
	if m.Sections == nil {
		m.Sections = []string{}
	}
	return m, err
}

func (r *ModemRepo) GetByID(ctx context.Context, id int64) (*types.Modem, error) {
	m, err := scanModem(r.pool.QueryRow(ctx, modemSelect+` WHERE m.id = $1`, id))
	return notFound(&m, err)
}

func (r *ModemRepo) GetBySerial(ctx context.Context, serial string) (*types.Modem, error) {
	m, err := scanModem(r.pool.QueryRow(ctx, modemSelect+` WHERE m.serial = $1`, serial))
	return notFound(&m, err)
}

const modemViewSelect = `SELECT ` + modemCols + `, a.name, (SELECT COUNT(*) FROM devices d WHERE d.modem_id = m.id)
	FROM modems m JOIN clients c ON c.id = m.client_id LEFT JOIN agents a ON a.id = m.agent_id`

func scanModemView(row rowScanner) (types.ModemView, error) {
	var v types.ModemView
	m := &v.Modem
	err := row.Scan(&m.ID, &m.ClientID, &m.Serial, &m.Name, &m.HasGenerator, &m.Site, &m.AgentID, &m.Connection,
		&m.Sections, &m.Status, &m.LastSeen, &m.CreatedAt, &m.ClientName, &v.AgentName, &v.DevicesCount)
	if m.Connection == nil {
		m.Connection = map[string]any{}
	}
	if m.Sections == nil {
		m.Sections = []string{}
	}
	return v, err
}

// GetView — модем (если ownerUserID != nil — только модем этого клиента).
func (r *ModemRepo) GetView(ctx context.Context, id int64, ownerUserID *int64) (*types.ModemView, error) {
	q := modemViewSelect + ` WHERE m.id = $1`
	args := []any{id}
	if ownerUserID != nil {
		q += ` AND c.user_id = $2`
		args = append(args, *ownerUserID)
	}
	v, err := scanModemView(r.pool.QueryRow(ctx, q, args...))
	return notFound(&v, err)
}

func (r *ModemRepo) ListViews(ctx context.Context, f types.ModemFilter) ([]types.ModemView, error) {
	var where []string
	var args []any
	if f.OwnerUserID != nil {
		args = append(args, *f.OwnerUserID)
		where = append(where, fmt.Sprintf("c.user_id = $%d", len(args)))
	}
	if f.ClientID != nil {
		args = append(args, *f.ClientID)
		where = append(where, fmt.Sprintf("m.client_id = $%d", len(args)))
	}
	if f.Search != "" {
		args = append(args, like(f.Search))
		where = append(where, fmt.Sprintf("m.serial ILIKE $%d", len(args)))
	}
	q := modemViewSelect
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	rows, err := r.pool.Query(ctx, q+` ORDER BY c.name, m.serial`, args...)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanModemView)
}

func (r *ModemRepo) list(ctx context.Context, where string, args ...any) ([]types.Modem, error) {
	rows, err := r.pool.Query(ctx, modemSelect+` WHERE `+where+` ORDER BY m.id`, args...)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanModem)
}

func (r *ModemRepo) ListByClient(ctx context.Context, clientID int64) ([]types.Modem, error) {
	return r.list(ctx, `m.client_id = $1`, clientID)
}

func (r *ModemRepo) ListByAgent(ctx context.Context, agentID int64) ([]types.Modem, error) {
	return r.list(ctx, `m.agent_id = $1`, agentID)
}

// ListForDemo — модемы для демо-режима: без локального агента и не облачные (настоящие не трогаем).
func (r *ModemRepo) ListForDemo(ctx context.Context) ([]types.Modem, error) {
	return r.list(ctx, `m.agent_id IS NULL AND COALESCE(m.connection->>'protocol', '') = ''`)
}

func (r *ModemRepo) ListByProtocol(ctx context.Context, protocol string) ([]types.Modem, error) {
	return r.list(ctx, `m.connection->>'protocol' = $1`, protocol)
}

// SerialTaken — модем с таким SN у другого клиента (excludeClientID=0 — у любого).
func (r *ModemRepo) SerialTaken(ctx context.Context, serial string, excludeClientID int64) (*types.Modem, error) {
	m, err := scanModem(r.pool.QueryRow(ctx, modemSelect+` WHERE m.serial = $1 AND m.client_id <> $2 LIMIT 1`,
		serial, excludeClientID))
	return notFound(&m, err)
}

func (r *ModemRepo) Create(ctx context.Context, m *types.Modem) error {
	return r.pool.QueryRow(ctx, `INSERT INTO modems (client_id, serial, name, has_generator, site, agent_id, connection, sections)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, status, created_at`,
		m.ClientID, m.Serial, m.Name, m.HasGenerator, m.Site, m.AgentID, jsonMap(m.Connection), jsonList(m.Sections),
	).Scan(&m.ID, &m.Status, &m.CreatedAt)
}

func (r *ModemRepo) Update(ctx context.Context, m *types.Modem) error {
	_, err := r.pool.Exec(ctx, `UPDATE modems SET client_id=$2, serial=$3, name=$4, has_generator=$5, site=$6,
		agent_id=$7, connection=$8, sections=$9 WHERE id=$1`,
		m.ID, m.ClientID, m.Serial, m.Name, m.HasGenerator, m.Site, m.AgentID, jsonMap(m.Connection), jsonList(m.Sections))
	return err
}

func (r *ModemRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM modems WHERE id = $1`, id)
	return err
}

func (r *ModemRepo) SetOnline(ctx context.Context, id int64, t time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE modems SET status = 'online', last_seen = $2 WHERE id = $1`, id, t)
	return err
}

func (r *ModemRepo) SetHasGenerator(ctx context.Context, id int64, v bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE modems SET has_generator = $2 WHERE id = $1`, id, v)
	return err
}

// ListStaleOnline — online-модемы без данных дольше таймаута.
// Для облачных (Deye Cloud) — свой, более длинный таймаут: облако обновляет данные раз в несколько минут.
func (r *ModemRepo) ListStaleOnline(ctx context.Context, before, cloudBefore time.Time) ([]types.Modem, error) {
	return r.list(ctx, `m.status = 'online' AND m.last_seen <
		CASE WHEN m.connection->>'protocol' = 'deye_cloud' THEN $2::timestamptz ELSE $1::timestamptz END`, before, cloudBefore)
}

// MarkOfflineIfOnline — update с фильтром status=online: если проверяют два процесса, аварию создаст только один.
func (r *ModemRepo) MarkOfflineIfOnline(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE modems SET status = 'offline' WHERE id = $1 AND status = 'online'`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
