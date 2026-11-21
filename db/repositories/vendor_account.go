package repositories

import (
	"context"
	"time"

	"ata-energo/types"

	"github.com/jackc/pgx/v5/pgxpool"
)

type VendorAccountRepo struct {
	pool *pgxpool.Pool
}

func NewVendorAccountRepo(pool *pgxpool.Pool) *VendorAccountRepo {
	return &VendorAccountRepo{pool: pool}
}

const vendorSelect = `SELECT v.id, v.vendor, v.name, v.base_url, v.app_id, v.app_secret, v.email, v.username,
	v.mobile, v.country_code, v.password_sha256, v.company_id, v.is_active, v.last_error, v.last_sync_at,
	v.created_at,
	(SELECT COUNT(*) FROM modems m WHERE m.connection->>'protocol' = 'deye_cloud'
		AND m.connection->>'account_id' = v.id::text)
	FROM vendor_accounts v`

func scanVendor(row rowScanner) (types.VendorAccount, error) {
	var a types.VendorAccount
	err := row.Scan(&a.ID, &a.Vendor, &a.Name, &a.BaseURL, &a.AppID, &a.AppSecret, &a.Email, &a.Username,
		&a.Mobile, &a.CountryCode, &a.PasswordSHA256, &a.CompanyID, &a.IsActive, &a.LastError, &a.LastSyncAt,
		&a.CreatedAt, &a.ModemsCount)
	return a, err
}

func (r *VendorAccountRepo) GetByID(ctx context.Context, id int64) (*types.VendorAccount, error) {
	a, err := scanVendor(r.pool.QueryRow(ctx, vendorSelect+` WHERE v.id = $1`, id))
	return notFound(&a, err)
}

func (r *VendorAccountRepo) List(ctx context.Context, vendor string) ([]types.VendorAccount, error) {
	rows, err := r.pool.Query(ctx, vendorSelect+` WHERE v.vendor = $1 ORDER BY v.name, v.id`, vendor)
	if err != nil {
		return nil, err
	}
	return collect(rows, scanVendor)
}

func (r *VendorAccountRepo) FindByAppAndLogin(ctx context.Context, vendor, appID, email, username, mobile string) (*types.VendorAccount, error) {
	a, err := scanVendor(r.pool.QueryRow(ctx, vendorSelect+` WHERE v.vendor = $1 AND v.app_id = $2
		AND v.email = $3 AND v.username = $4 AND v.mobile = $5 LIMIT 1`, vendor, appID, email, username, mobile))
	return notFound(&a, err)
}

func (r *VendorAccountRepo) Create(ctx context.Context, a *types.VendorAccount) error {
	return r.pool.QueryRow(ctx, `INSERT INTO vendor_accounts (vendor, name, base_url, app_id, app_secret, email,
		username, mobile, country_code, password_sha256, company_id, is_active)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id, created_at`,
		a.Vendor, a.Name, a.BaseURL, a.AppID, a.AppSecret, a.Email, a.Username, a.Mobile, a.CountryCode,
		a.PasswordSHA256, a.CompanyID, a.IsActive).Scan(&a.ID, &a.CreatedAt)
}

func (r *VendorAccountRepo) Update(ctx context.Context, a *types.VendorAccount) error {
	_, err := r.pool.Exec(ctx, `UPDATE vendor_accounts SET name=$2, base_url=$3, app_id=$4, app_secret=$5,
		email=$6, username=$7, mobile=$8, country_code=$9, password_sha256=$10, company_id=$11, is_active=$12
		WHERE id=$1`,
		a.ID, a.Name, a.BaseURL, a.AppID, a.AppSecret, a.Email, a.Username, a.Mobile, a.CountryCode,
		a.PasswordSHA256, a.CompanyID, a.IsActive)
	return err
}

func (r *VendorAccountRepo) SetSyncResult(ctx context.Context, id int64, lastError string, t time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE vendor_accounts SET last_error=$2, last_sync_at=$3 WHERE id=$1`, id, lastError, t)
	return err
}

func (r *VendorAccountRepo) Delete(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM vendor_accounts WHERE id = $1`, id)
	return err
}
