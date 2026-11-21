package repositories

import (
	"context"

	"ata-energo/types"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ClientRepo struct {
	pool *pgxpool.Pool
}

func NewClientRepo(pool *pgxpool.Pool) *ClientRepo {
	return &ClientRepo{pool: pool}
}

func scanClient(row rowScanner) (types.Client, error) {
	var c types.Client
	err := row.Scan(&c.ID, &c.UserID, &c.Name, &c.Phone, &c.Address, &c.CreatedAt)
	return c, err
}

func (r *ClientRepo) GetByID(ctx context.Context, id int64) (*types.Client, error) {
	c, err := scanClient(r.pool.QueryRow(ctx,
		`SELECT id, user_id, name, phone, address, created_at FROM clients WHERE id = $1`, id))
	return notFound(&c, err)
}

// Клиент + логин + количество устройств (всего и online) одним запросом.
const clientViewSelect = `SELECT c.id, c.user_id, c.name, c.phone, c.address, c.created_at, u.username,
	(SELECT COUNT(*) FROM devices d WHERE d.client_id = c.id),
	(SELECT COUNT(*) FROM devices d WHERE d.client_id = c.id AND d.status = 'online')
	FROM clients c LEFT JOIN users u ON u.id = c.user_id`

func scanClientView(row rowScanner) (types.ClientView, error) {
	var v types.ClientView
	err := row.Scan(&v.ID, &v.UserID, &v.Name, &v.Phone, &v.Address, &v.CreatedAt, &v.Username,
		&v.DevicesCount, &v.OnlineCount)
	v.Modems = []types.ClientModemShort{}
	return v, err
}

func (r *ClientRepo) GetView(ctx context.Context, id int64) (*types.ClientView, error) {
	v, err := scanClientView(r.pool.QueryRow(ctx, clientViewSelect+` WHERE c.id = $1`, id))
	if err != nil {
		return notFound(&v, err)
	}
	list := []types.ClientView{v}
	if err := r.attachModems(ctx, list); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// ListViews — клиенты (поиск по имени, телефону, адресу и SN модема).
func (r *ClientRepo) ListViews(ctx context.Context, search string) ([]types.ClientView, error) {
	q := clientViewSelect
	var args []any
	if search != "" {
		args = append(args, like(search))
		q += ` WHERE c.name ILIKE $1 OR c.phone ILIKE $1 OR c.address ILIKE $1
			OR EXISTS (SELECT 1 FROM modems m WHERE m.client_id = c.id AND m.serial ILIKE $1)`
	}
	rows, err := r.pool.Query(ctx, q+` ORDER BY c.name, c.id`, args...)
	if err != nil {
		return nil, err
	}
	list, err := collect(rows, scanClientView)
	if err != nil {
		return nil, err
	}
	if err := r.attachModems(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
}

// attachModems — короткий список модемов каждого клиента (один запрос на всех).
func (r *ClientRepo) attachModems(ctx context.Context, list []types.ClientView) error {
	if len(list) == 0 {
		return nil
	}
	ids := make([]int64, len(list))
	index := make(map[int64]int, len(list))
	for i, c := range list {
		ids[i] = c.ID
		index[c.ID] = i
	}
	rows, err := r.pool.Query(ctx, `SELECT client_id, id, serial, name, has_generator, site, status, last_seen
		FROM modems WHERE client_id = ANY($1) ORDER BY serial`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var clientID int64
		var m types.ClientModemShort
		if err := rows.Scan(&clientID, &m.ID, &m.Serial, &m.Name, &m.HasGenerator, &m.Site, &m.Status, &m.LastSeen); err != nil {
			return err
		}
		i := index[clientID]
		list[i].Modems = append(list[i].Modems, m)
	}
	return rows.Err()
}

func (r *ClientRepo) Create(ctx context.Context, c *types.Client) error {
	return r.pool.QueryRow(ctx, `INSERT INTO clients (user_id, name, phone, address) VALUES ($1,$2,$3,$4)
		RETURNING id, created_at`, c.UserID, c.Name, c.Phone, c.Address).Scan(&c.ID, &c.CreatedAt)
}

func (r *ClientRepo) Update(ctx context.Context, c *types.Client) error {
	_, err := r.pool.Exec(ctx, `UPDATE clients SET user_id=$2, name=$3, phone=$4, address=$5 WHERE id=$1`,
		c.ID, c.UserID, c.Name, c.Phone, c.Address)
	return err
}

func (r *ClientRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM clients WHERE id = $1`, id)
	return err
}
