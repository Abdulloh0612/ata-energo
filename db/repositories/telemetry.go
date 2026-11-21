package repositories

import (
	"context"
	"time"

	"ata-energo/types"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TelemetryRepo struct {
	pool *pgxpool.Pool
}

func NewTelemetryRepo(pool *pgxpool.Pool) *TelemetryRepo {
	return &TelemetryRepo{pool: pool}
}

func (r *TelemetryRepo) Insert(ctx context.Context, deviceID int64, ts time.Time, v types.Values, errText *string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO telemetry (device_id, timestamp, current, voltage, power, battery_percent, temperature, error)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		deviceID, ts, v.Current, v.Voltage, v.Power, v.BatteryPercent, v.Temperature, errText)
	return err
}

// History — точки за период. Если точек больше limit — берём каждую N-ю (прореживание),
// чтобы график не тормозил. Возвращает точки и общее количество.
func (r *TelemetryRepo) History(ctx context.Context, deviceID int64, from, to time.Time, limit int) ([]types.HistoryPoint, int, error) {
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM telemetry WHERE device_id = $1 AND timestamp BETWEEN $2 AND $3`,
		deviceID, from, to).Scan(&total); err != nil {
		return nil, 0, err
	}
	step := 1
	if total > limit {
		step = total/limit + 1
	}
	rows, err := r.pool.Query(ctx, `SELECT timestamp, current, voltage, power, battery_percent, temperature, error FROM (
			SELECT *, ROW_NUMBER() OVER (ORDER BY timestamp, id) - 1 AS rn
			FROM telemetry WHERE device_id = $1 AND timestamp BETWEEN $2 AND $3
		) t WHERE rn % $4 = 0 ORDER BY timestamp`, deviceID, from, to, step)
	if err != nil {
		return nil, 0, err
	}
	points, err := collect(rows, func(row rowScanner) (types.HistoryPoint, error) {
		var p types.HistoryPoint
		err := row.Scan(&p.Timestamp, &p.Current, &p.Voltage, &p.Power, &p.BatteryPercent, &p.Temperature, &p.Error)
		return p, err
	})
	return points, total, err
}

// ListForOwner — последние показания устройств клиента (новые сверху).
func (r *TelemetryRepo) ListForOwner(ctx context.Context, ownerUserID int64, deviceID *int64, limit int) ([]types.Telemetry, error) {
	q := `SELECT t.id, t.device_id, d.device_id, d.device_type, t.timestamp, t.current, t.voltage, t.power,
		t.battery_percent, t.temperature, t.error
		FROM telemetry t JOIN devices d ON d.id = t.device_id JOIN clients c ON c.id = d.client_id
		WHERE c.user_id = $1`
	args := []any{ownerUserID, limit}
	if deviceID != nil {
		q += ` AND t.device_id = $3`
		args = append(args, *deviceID)
	}
	rows, err := r.pool.Query(ctx, q+` ORDER BY t.timestamp DESC LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(row rowScanner) (types.Telemetry, error) {
		var t types.Telemetry
		err := row.Scan(&t.ID, &t.DeviceRef, &t.DeviceCode, &t.DeviceType, &t.Timestamp, &t.Current, &t.Voltage,
			&t.Power, &t.BatteryPercent, &t.Temperature, &t.Error)
		return t, err
	})
}
