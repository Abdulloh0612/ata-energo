package repositories

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ata-energo/types"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo {
	return &UserRepo{pool: pool}
}

const userSelect = `SELECT u.id, u.username, u.password_hash, u.first_name, u.last_name, u.email, u.phone,
	u.position, u.role, u.is_active, u.is_superuser, u.date_joined, u.last_login, c.id
	FROM users u LEFT JOIN clients c ON c.user_id = u.id`

func scanUser(row rowScanner) (types.User, error) {
	var u types.User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.FirstName, &u.LastName, &u.Email, &u.Phone,
		&u.Position, &u.Role, &u.IsActive, &u.IsSuperuser, &u.DateJoined, &u.LastLogin, &u.ClientID)
	return u, err
}

func (r *UserRepo) GetByID(ctx context.Context, id int64) (*types.User, error) {
	u, err := scanUser(r.pool.QueryRow(ctx, userSelect+` WHERE u.id = $1`, id))
	return notFound(&u, err)
}

func (r *UserRepo) GetByUsername(ctx context.Context, username string) (*types.User, error) {
	u, err := scanUser(r.pool.QueryRow(ctx, userSelect+` WHERE u.username = $1`, username))
	return notFound(&u, err)
}

func (r *UserRepo) UsernameTaken(ctx context.Context, username string, excludeID int64) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username = $1 AND id <> $2)`,
		username, excludeID).Scan(&exists)
	return exists, err
}

// List — пользователи с фильтрами (?role=, ?staff=1, ?search=).
func (r *UserRepo) List(ctx context.Context, f types.UserFilter) ([]types.User, error) {
	var where []string
	var args []any
	if f.Role != "" {
		args = append(args, f.Role)
		where = append(where, fmt.Sprintf("u.role = $%d", len(args)))
	}
	if f.Staff {
		where = append(where, "u.role <> 'user'")
	}
	if f.Search != "" {
		args = append(args, like(f.Search))
		n := len(args)
		where = append(where, fmt.Sprintf("(u.username ILIKE $%d OR u.first_name ILIKE $%d OR u.last_name ILIKE $%d OR u.phone ILIKE $%d)", n, n, n, n))
	}
	q := userSelect
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	rows, err := r.pool.Query(ctx, q+" ORDER BY u.id", args...)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanUser)
}

func (r *UserRepo) Create(ctx context.Context, u *types.User) error {
	return r.pool.QueryRow(ctx, `INSERT INTO users
		(username, password_hash, first_name, last_name, email, phone, position, role, is_active, is_superuser)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id, date_joined`,
		u.Username, u.PasswordHash, u.FirstName, u.LastName, u.Email, u.Phone, u.Position, u.Role,
		u.IsActive, u.IsSuperuser,
	).Scan(&u.ID, &u.DateJoined)
}

func (r *UserRepo) Update(ctx context.Context, u *types.User) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET username=$2, password_hash=$3, first_name=$4, last_name=$5,
		email=$6, phone=$7, position=$8, role=$9, is_active=$10 WHERE id=$1`,
		u.ID, u.Username, u.PasswordHash, u.FirstName, u.LastName, u.Email, u.Phone, u.Position, u.Role, u.IsActive)
	return err
}

func (r *UserRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	return err
}

func (r *UserRepo) SetLastLogin(ctx context.Context, id int64, t time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE users SET last_login = $2 WHERE id = $1`, id, t)
	return err
}
