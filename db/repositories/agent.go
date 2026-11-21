package repositories

import (
	"context"
	"time"

	"ata-energo/types"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ─── Agents ──────────────────────────────────────────────────────────────────

type AgentRepo struct {
	pool *pgxpool.Pool
}

func NewAgentRepo(pool *pgxpool.Pool) *AgentRepo {
	return &AgentRepo{pool: pool}
}

const agentSelect = `SELECT a.id, a.name, a.client_id, c.name, a.token_hash, a.token_hint, a.hostname, a.ip, a.local_ip,
	a.platform, a.version, a.capabilities, a.last_seen, a.is_active, a.created_at
	FROM agents a LEFT JOIN clients c ON c.id = a.client_id`

func scanAgent(row rowScanner) (types.Agent, error) {
	var a types.Agent
	err := row.Scan(&a.ID, &a.Name, &a.ClientID, &a.ClientName, &a.TokenHash, &a.TokenHint, &a.Hostname, &a.IP,
		&a.LocalIP, &a.Platform, &a.Version, &a.Capabilities, &a.LastSeen, &a.IsActive, &a.CreatedAt)
	if a.Capabilities == nil {
		a.Capabilities = map[string]any{}
	}
	a.ComputeOnline()
	return a, err
}

func (r *AgentRepo) GetByID(ctx context.Context, id int64) (*types.Agent, error) {
	a, err := scanAgent(r.pool.QueryRow(ctx, agentSelect+` WHERE a.id = $1`, id))
	return notFound(&a, err)
}

func (r *AgentRepo) GetActiveByTokenHash(ctx context.Context, hash string) (*types.Agent, error) {
	a, err := scanAgent(r.pool.QueryRow(ctx, agentSelect+` WHERE a.token_hash = $1 AND a.is_active`, hash))
	return notFound(&a, err)
}

func (r *AgentRepo) List(ctx context.Context) ([]types.Agent, error) {
	rows, err := r.pool.Query(ctx, agentSelect+` ORDER BY a.name, a.id`)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanAgent)
}

func (r *AgentRepo) Create(ctx context.Context, a *types.Agent) error {
	return r.pool.QueryRow(ctx, `INSERT INTO agents (name, client_id, token_hash, token_hint, is_active)
		VALUES ($1,$2,$3,$4,$5) RETURNING id, created_at`,
		a.Name, a.ClientID, a.TokenHash, a.TokenHint, a.IsActive).Scan(&a.ID, &a.CreatedAt)
}

func (r *AgentRepo) Update(ctx context.Context, a *types.Agent) error {
	_, err := r.pool.Exec(ctx, `UPDATE agents SET name=$2, client_id=$3, is_active=$4 WHERE id=$1`,
		a.ID, a.Name, a.ClientID, a.IsActive)
	return err
}

func (r *AgentRepo) SetToken(ctx context.Context, id int64, hash, hint string) error {
	_, err := r.pool.Exec(ctx, `UPDATE agents SET token_hash=$2, token_hint=$3 WHERE id=$1`, id, hash, hint)
	return err
}

func (r *AgentRepo) Touch(ctx context.Context, id int64, ip string, t time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE agents SET last_seen=$2, ip=$3 WHERE id=$1`, id, t, ip)
	return err
}

func (r *AgentRepo) SetHello(ctx context.Context, a *types.Agent) error {
	_, err := r.pool.Exec(ctx, `UPDATE agents SET hostname=$2, platform=$3, version=$4, local_ip=$5, capabilities=$6 WHERE id=$1`,
		a.ID, a.Hostname, a.Platform, a.Version, a.LocalIP, jsonMap(a.Capabilities))
	return err
}

func (r *AgentRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM agents WHERE id = $1`, id)
	return err
}

// ─── Agent tasks ─────────────────────────────────────────────────────────────

type AgentTaskRepo struct {
	pool *pgxpool.Pool
}

func NewAgentTaskRepo(pool *pgxpool.Pool) *AgentTaskRepo {
	return &AgentTaskRepo{pool: pool}
}

const taskSelect = `SELECT t.id, t.agent_id, a.last_seen, t.kind, t.method, t.params, t.device_id, t.status, t.result,
	t.error, t.log, t.created_by, t.created_at, t.started_at, t.finished_at
	FROM agent_tasks t JOIN agents a ON a.id = t.agent_id`

func scanTask(row rowScanner) (types.AgentTask, error) {
	var t types.AgentTask
	var agentSeen *time.Time
	err := row.Scan(&t.ID, &t.AgentID, &agentSeen, &t.Kind, &t.Method, &t.Params, &t.DeviceID, &t.Status, &t.Result,
		&t.Error, &t.Log, &t.CreatedBy, &t.CreatedAt, &t.StartedAt, &t.FinishedAt)
	t.AgentOnline = agentSeen != nil && time.Since(*agentSeen) < types.AgentOnlineSeconds*time.Second
	if t.Params == nil {
		t.Params = map[string]any{}
	}
	if t.Result == nil {
		t.Result = map[string]any{}
	}
	t.Found = []types.FoundDevice{}
	return t, err
}

func (r *AgentTaskRepo) GetByID(ctx context.Context, id int64) (*types.AgentTask, error) {
	t, err := scanTask(r.pool.QueryRow(ctx, taskSelect+` WHERE t.id = $1`, id))
	return notFound(&t, err)
}

func (r *AgentTaskRepo) Create(ctx context.Context, t *types.AgentTask) error {
	if err := r.pool.QueryRow(ctx, `INSERT INTO agent_tasks (agent_id, kind, method, params, device_id, created_by)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		t.AgentID, t.Kind, t.Method, jsonMap(t.Params), t.DeviceID, t.CreatedBy).Scan(&t.ID); err != nil {
		return err
	}
	saved, err := r.GetByID(ctx, t.ID)
	if err != nil || saved == nil {
		return err
	}
	*t = *saved
	return nil
}

// TakePending — отдаёт агенту новые задачи и помечает их "выполняется".
func (r *AgentTaskRepo) TakePending(ctx context.Context, agentID int64, limit int) ([]types.AgentTask, error) {
	rows, err := r.pool.Query(ctx, `UPDATE agent_tasks SET status = 'running', started_at = NOW()
		WHERE id IN (SELECT id FROM agent_tasks WHERE agent_id = $1 AND status = 'pending'
			ORDER BY created_at LIMIT $2 FOR UPDATE SKIP LOCKED)
		RETURNING id, kind, method, params, created_at`, agentID, limit)
	if err != nil {
		return nil, err
	}
	tasks, err := collect(rows, func(row rowScanner) (types.AgentTask, error) {
		var t types.AgentTask
		err := row.Scan(&t.ID, &t.Kind, &t.Method, &t.Params, &t.CreatedAt)
		if t.Params == nil {
			t.Params = map[string]any{}
		}
		return t, err
	})
	if err != nil {
		return nil, err
	}
	// по порядку создания
	for i := 1; i < len(tasks); i++ {
		for j := i; j > 0 && tasks[j].CreatedAt.Before(tasks[j-1].CreatedAt); j-- {
			tasks[j], tasks[j-1] = tasks[j-1], tasks[j]
		}
	}
	return tasks, nil
}

// AppendLog — строка в журнал задачи (храним последние 20000 символов).
func (r *AgentTaskRepo) AppendLog(ctx context.Context, id int64, line string) error {
	_, err := r.pool.Exec(ctx, `UPDATE agent_tasks SET log = RIGHT(log || $2 || E'\n', 20000) WHERE id = $1`, id, line)
	return err
}

func (r *AgentTaskRepo) Finish(ctx context.Context, t *types.AgentTask) error {
	_, err := r.pool.Exec(ctx, `UPDATE agent_tasks SET status=$2, error=$3, result=$4, finished_at=$5 WHERE id=$1`,
		t.ID, t.Status, t.Error, jsonMap(t.Result), t.FinishedAt)
	return err
}

// ─── Found devices ───────────────────────────────────────────────────────────

type FoundDeviceRepo struct {
	pool *pgxpool.Pool
}

func NewFoundDeviceRepo(pool *pgxpool.Pool) *FoundDeviceRepo {
	return &FoundDeviceRepo{pool: pool}
}

const foundSelect = `SELECT f.id, f.agent_id, a.name, f.task_id, f.source, f.key, f.connection_type, f.name,
	f.manufacturer, f.model, f.serial, f.ip, f.mac, f.bt_address, f.rssi, f.device_kind, f.protocols, f.details,
	f.status, f.check_result, f.modem_id, m.serial, m.client_id, f.first_seen, f.last_seen
	FROM found_devices f LEFT JOIN agents a ON a.id = f.agent_id LEFT JOIN modems m ON m.id = f.modem_id`

func scanFound(row rowScanner) (types.FoundDevice, error) {
	var d types.FoundDevice
	err := row.Scan(&d.ID, &d.AgentID, &d.AgentName, &d.TaskID, &d.Source, &d.Key, &d.ConnectionType, &d.Name,
		&d.Manufacturer, &d.Model, &d.Serial, &d.IP, &d.MAC, &d.BTAddress, &d.RSSI, &d.DeviceKind, &d.Protocols,
		&d.Details, &d.Status, &d.CheckResult, &d.ModemID, &d.ModemSerial, &d.ModemClient, &d.FirstSeen, &d.LastSeen)
	if d.Protocols == nil {
		d.Protocols = []string{}
	}
	if d.Details == nil {
		d.Details = map[string]any{}
	}
	if d.CheckResult == nil {
		d.CheckResult = map[string]any{}
	}
	d.FillDisplay()
	return d, err
}

func (r *FoundDeviceRepo) GetByID(ctx context.Context, id int64) (*types.FoundDevice, error) {
	d, err := scanFound(r.pool.QueryRow(ctx, foundSelect+` WHERE f.id = $1`, id))
	return notFound(&d, err)
}

func (r *FoundDeviceRepo) GetByAgentKey(ctx context.Context, agentID *int64, key string) (*types.FoundDevice, error) {
	d, err := scanFound(r.pool.QueryRow(ctx, foundSelect+` WHERE COALESCE(f.agent_id, 0) = COALESCE($1::bigint, 0) AND f.key = $2`,
		agentID, key))
	return notFound(&d, err)
}

// List — найденные устройства (новые сверху). agentID != nil — этого агента + найденные браузером.
func (r *FoundDeviceRepo) List(ctx context.Context, agentID *int64, limit int) ([]types.FoundDevice, error) {
	q := foundSelect
	args := []any{limit}
	if agentID != nil {
		q += ` WHERE f.agent_id = $2 OR f.agent_id IS NULL`
		args = append(args, *agentID)
	}
	rows, err := r.pool.Query(ctx, q+` ORDER BY f.last_seen DESC LIMIT $1`, args...)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanFound)
}

func (r *FoundDeviceRepo) ListByTask(ctx context.Context, taskID int64) ([]types.FoundDevice, error) {
	rows, err := r.pool.Query(ctx, foundSelect+` WHERE f.task_id = $1 ORDER BY f.device_kind, f.ip, f.name`, taskID)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanFound)
}

// Create — новое найденное устройство (если уже есть по agent+key — ничего не делает).
func (r *FoundDeviceRepo) Create(ctx context.Context, d *types.FoundDevice) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO found_devices (agent_id, key, connection_type, source)
		VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, d.AgentID, d.Key, d.ConnectionType, d.Source)
	return err
}

func (r *FoundDeviceRepo) Save(ctx context.Context, d *types.FoundDevice) error {
	_, err := r.pool.Exec(ctx, `UPDATE found_devices SET task_id=$2, source=$3, connection_type=$4, name=$5,
		manufacturer=$6, model=$7, serial=$8, ip=$9, mac=$10, bt_address=$11, rssi=$12, device_kind=$13,
		protocols=$14, details=$15, status=$16, check_result=$17, modem_id=$18, last_seen=$19 WHERE id=$1`,
		d.ID, d.TaskID, d.Source, d.ConnectionType, d.Name, d.Manufacturer, d.Model, d.Serial, d.IP, d.MAC,
		d.BTAddress, d.RSSI, d.DeviceKind, jsonList(d.Protocols), jsonMap(d.Details), d.Status,
		jsonMap(d.CheckResult), d.ModemID, d.LastSeen)
	return err
}

func (r *FoundDeviceRepo) DeleteNotConnected(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM found_devices WHERE status <> 'connected'`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
