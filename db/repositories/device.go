package repositories

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ata-energo/types"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DeviceRepo struct {
	pool *pgxpool.Pool
}

func NewDeviceRepo(pool *pgxpool.Pool) *DeviceRepo {
	return &DeviceRepo{pool: pool}
}

const deviceSelect = `SELECT d.id, d.client_id, d.modem_id, d.device_type, d.name, d.serial_number, d.device_id,
	d.status, d.last_seen, d.last_data, d.created_at, c.name, m.serial
	FROM devices d JOIN clients c ON c.id = d.client_id LEFT JOIN modems m ON m.id = d.modem_id`

func scanDevice(row rowScanner) (types.Device, error) {
	var d types.Device
	err := row.Scan(&d.ID, &d.ClientID, &d.ModemID, &d.DeviceType, &d.Name, &d.SerialNumber, &d.DeviceID,
		&d.Status, &d.LastSeen, &d.LastData, &d.CreatedAt, &d.ClientName, &d.ModemSerial)
	if d.LastData == nil {
		d.LastData = map[string]any{}
	}
	return d, err
}

// GetByID — устройство (если ownerUserID != nil — только устройство этого клиента, иначе nil = 404).
func (r *DeviceRepo) GetByID(ctx context.Context, id int64, ownerUserID *int64) (*types.Device, error) {
	q := deviceSelect + ` WHERE d.id = $1`
	args := []any{id}
	if ownerUserID != nil {
		q += ` AND c.user_id = $2`
		args = append(args, *ownerUserID)
	}
	d, err := scanDevice(r.pool.QueryRow(ctx, q, args...))
	return notFound(&d, err)
}

func (r *DeviceRepo) GetByDeviceID(ctx context.Context, deviceID string) (*types.Device, error) {
	d, err := scanDevice(r.pool.QueryRow(ctx, deviceSelect+` WHERE d.device_id = $1`, deviceID))
	return notFound(&d, err)
}

// List — устройства, доступные пользователю, + фильтры (?client=, ?type=, ?status=).
func (r *DeviceRepo) List(ctx context.Context, f types.DeviceFilter) ([]types.Device, error) {
	var where []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.OwnerUserID != nil {
		add("c.user_id = $%d", *f.OwnerUserID)
	}
	if f.ClientID != nil {
		add("d.client_id = $%d", *f.ClientID)
	}
	if f.Type != "" {
		add("d.device_type = $%d", f.Type)
	}
	if f.Status != "" {
		add("d.status = $%d", f.Status)
	}
	q := deviceSelect
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	rows, err := r.pool.Query(ctx, q+` ORDER BY c.name, d.device_type, d.device_id`, args...)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanDevice)
}

func (r *DeviceRepo) ListByModem(ctx context.Context, modemID int64) ([]types.Device, error) {
	rows, err := r.pool.Query(ctx, deviceSelect+` WHERE d.modem_id = $1 ORDER BY d.device_type, d.device_id`, modemID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanDevice)
}

func (r *DeviceRepo) CountByModem(ctx context.Context, modemID int64) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE modem_id = $1`, modemID).Scan(&n)
	return n, err
}

// FirstByModem — главное устройство модема (инвертор, иначе любое).
func (r *DeviceRepo) FirstByModem(ctx context.Context, modemID int64) (*types.Device, error) {
	d, err := scanDevice(r.pool.QueryRow(ctx, deviceSelect+` WHERE d.modem_id = $1
		ORDER BY (d.device_type = 'inverter') DESC, d.device_type, d.device_id LIMIT 1`, modemID))
	return notFound(&d, err)
}

// GetOrCreate — находит устройство по device_id или создаёт его.
func (r *DeviceRepo) GetOrCreate(ctx context.Context, d *types.Device) (*types.Device, error) {
	if _, err := r.pool.Exec(ctx, `INSERT INTO devices (client_id, modem_id, device_type, name, serial_number, device_id)
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`,
		d.ClientID, d.ModemID, d.DeviceType, d.Name, d.SerialNumber, d.DeviceID); err != nil {
		return nil, err
	}
	return r.GetByDeviceID(ctx, d.DeviceID)
}

func (r *DeviceRepo) Create(ctx context.Context, d *types.Device) error {
	return r.pool.QueryRow(ctx, `INSERT INTO devices (client_id, modem_id, device_type, name, serial_number, device_id)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, status, created_at`,
		d.ClientID, d.ModemID, d.DeviceType, d.Name, d.SerialNumber, d.DeviceID,
	).Scan(&d.ID, &d.Status, &d.CreatedAt)
}

func (r *DeviceRepo) Update(ctx context.Context, d *types.Device) error {
	_, err := r.pool.Exec(ctx, `UPDATE devices SET client_id=$2, modem_id=$3, device_type=$4, name=$5,
		serial_number=$6, device_id=$7 WHERE id=$1`,
		d.ID, d.ClientID, d.ModemID, d.DeviceType, d.Name, d.SerialNumber, d.DeviceID)
	return err
}

func (r *DeviceRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM devices WHERE id = $1`, id)
	return err
}

func (r *DeviceRepo) DeleteByModemExceptTypes(ctx context.Context, modemID int64, keep []string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM devices WHERE modem_id = $1 AND NOT (device_type = ANY($2))`, modemID, keep)
	return err
}

func (r *DeviceRepo) DeleteByModemType(ctx context.Context, modemID int64, deviceType string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM devices WHERE modem_id = $1 AND device_type = $2`, modemID, deviceType)
	return err
}

// ReassignModemClient — если модем перенесли к другому клиенту, переносим и его устройства.
func (r *DeviceRepo) ReassignModemClient(ctx context.Context, modemID, clientID int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE devices SET client_id = $2 WHERE modem_id = $1 AND client_id <> $2`, modemID, clientID)
	return err
}

// SaveLast — сохраняет статус, время и последние показания устройства.
func (r *DeviceRepo) SaveLast(ctx context.Context, d *types.Device) error {
	_, err := r.pool.Exec(ctx, `UPDATE devices SET status=$2, last_seen=$3, last_data=$4 WHERE id=$1`,
		d.ID, d.Status, d.LastSeen, jsonMap(d.LastData))
	return err
}

func (r *DeviceRepo) ListStaleOnline(ctx context.Context, before, cloudBefore time.Time) ([]types.Device, error) {
	rows, err := r.pool.Query(ctx, deviceSelect+` WHERE d.status = 'online' AND d.last_seen <
		CASE WHEN m.connection->>'protocol' IN ('deye_cloud','valueclouds_cloud') THEN $2::timestamptz ELSE $1::timestamptz END
		ORDER BY d.id`, before, cloudBefore)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanDevice)
}

func (r *DeviceRepo) MarkOfflineIfOnline(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE devices SET status = 'offline' WHERE id = $1 AND status = 'online'`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
