package repositories

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strconv"
	"time"

	"ata-energo/pkg/credentialbox"
	"ata-energo/types"

	"github.com/jackc/pgx/v5/pgxpool"
)

type VendorAccountRepo struct {
	pool *pgxpool.Pool
	box  *credentialbox.Box
}

func NewVendorAccountRepo(pool *pgxpool.Pool, box *credentialbox.Box) *VendorAccountRepo {
	return &VendorAccountRepo{pool: pool, box: box}
}

type credentialFields struct {
	AppID, AppSecret, Email, Username, Mobile, CountryCode, PasswordSHA256 string
	CompanyID                                                              *int64
}

func fieldsOf(a types.VendorAccount) credentialFields {
	return credentialFields{a.AppID, a.AppSecret, a.Email, a.Username, a.Mobile, a.CountryCode, a.PasswordSHA256, a.CompanyID}
}

func (f credentialFields) apply(a *types.VendorAccount) {
	a.AppID, a.AppSecret, a.Email, a.Username, a.Mobile, a.CountryCode, a.PasswordSHA256, a.CompanyID =
		f.AppID, f.AppSecret, f.Email, f.Username, f.Mobile, f.CountryCode, f.PasswordSHA256, f.CompanyID
}

func accountAAD(a types.VendorAccount) []byte {
	return []byte(fmt.Sprintf("vendor_accounts:%s:%d", a.Vendor, a.ID))
}

func (r *VendorAccountRepo) seal(a types.VendorAccount) ([]byte, error) {
	data, err := json.Marshal(fieldsOf(a))
	if err != nil {
		return nil, err
	}
	return r.box.Seal(data, accountAAD(a))
}

const vendorSelect = `SELECT v.id, v.vendor, v.name, v.base_url, v.fields_encrypted, v.key_version,
	v.is_active, EXISTS(SELECT 1 FROM providers p WHERE p.code=v.vendor AND p.enabled), v.last_error, v.last_sync_at, v.created_at,
	(SELECT COUNT(*) FROM modems m WHERE m.connection->>'protocol' = 'deye_cloud'
		AND m.connection->>'account_id' = v.id::text)
	FROM vendor_accounts v`

func (r *VendorAccountRepo) scanVendor(row rowScanner) (types.VendorAccount, error) {
	var a types.VendorAccount
	var sealed []byte
	var version *string
	err := row.Scan(&a.ID, &a.Vendor, &a.Name, &a.BaseURL, &sealed, &version, &a.IsActive, &a.ProviderEnabled, &a.LastError, &a.LastSyncAt, &a.CreatedAt, &a.ModemsCount)
	if err != nil {
		return a, err
	}
	if err = r.open(&a, version, sealed); err != nil {
		return a, err
	}
	a.HasCredentials = a.AppID != "" && a.AppSecret != "" && a.PasswordSHA256 != ""
	return a, nil
}

func (r *VendorAccountRepo) open(a *types.VendorAccount, version *string, sealed []byte) error {
	if version == nil {
		return fmt.Errorf("аккаунт %d не перенесён в зашифрованное хранилище", a.ID)
	}
	clear, err := r.box.Open(*version, sealed, accountAAD(*a))
	if err != nil {
		return err
	}
	var fields credentialFields
	if err := json.Unmarshal(clear, &fields); err != nil {
		return fmt.Errorf("не удалось прочитать учётные данные аккаунта %d", a.ID)
	}
	fields.apply(a)
	return nil
}

// Prepare migrates old records and rotates credentials atomically before the server starts.
func (r *VendorAccountRepo) Prepare(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `LOCK TABLE vendor_accounts IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT id,vendor,app_id,app_secret,email,username,mobile,country_code,password_sha256,company_id,fields_encrypted,key_version
		FROM vendor_accounts`)
	if err != nil {
		return err
	}
	type account struct {
		types.VendorAccount
		sealed  []byte
		version *string
	}
	var accounts []account
	for rows.Next() {
		var row account
		if err := rows.Scan(&row.ID, &row.Vendor, &row.AppID, &row.AppSecret, &row.Email, &row.Username, &row.Mobile,
			&row.CountryCode, &row.PasswordSHA256, &row.CompanyID, &row.sealed, &row.version); err != nil {
			rows.Close()
			return err
		}
		accounts = append(accounts, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, row := range accounts {
		if row.version != nil {
			if err := r.open(&row.VendorAccount, row.version, row.sealed); err != nil {
				return err
			}
			if *row.version == r.box.Version() {
				continue
			}
		}
		sealed, err := r.seal(row.VendorAccount)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE vendor_accounts SET fields_encrypted=$2,key_version=$3,app_id='',app_secret='',email='',
			username='',mobile='',country_code='',password_sha256='',company_id=NULL WHERE id=$1`, row.ID, sealed, r.box.Version()); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *VendorAccountRepo) GetByID(ctx context.Context, id int64) (*types.VendorAccount, error) {
	a, err := r.scanVendor(r.pool.QueryRow(ctx, vendorSelect+` WHERE v.id = $1`, id))
	return notFound(&a, err)
}

func (r *VendorAccountRepo) List(ctx context.Context, vendor string) ([]types.VendorAccount, error) {
	rows, err := r.pool.Query(ctx, vendorSelect+` WHERE v.vendor = $1 ORDER BY v.name,v.id`, vendor)
	if err != nil {
		return nil, err
	}
	return collect(rows, r.scanVendor)
}

func (r *VendorAccountRepo) FindByAppAndLogin(ctx context.Context, vendor, appID, email, username, mobile string) (*types.VendorAccount, error) {
	list, err := r.List(ctx, vendor)
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		if a.AppID == appID && a.Email == email && a.Username == username && a.Mobile == mobile {
			return &a, nil
		}
	}
	return nil, nil
}

type vendorActorKey struct{}

func WithVendorActor(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, vendorActorKey{}, id)
}

func (r *VendorAccountRepo) write(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if id, ok := ctx.Value(vendorActorKey{}).(int64); ok {
		if _, err = tx.Exec(ctx, `SELECT set_config('ata.provider_actor',$1,true)`, strconv.FormatInt(id, 10)); err != nil {
			return err
		}
	}
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *VendorAccountRepo) Create(ctx context.Context, a *types.VendorAccount) error {
	return r.write(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('vendor_accounts','id'))`).Scan(&a.ID); err != nil {
			return err
		}
		sealed, err := r.seal(*a)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO vendor_accounts (id,vendor,name,base_url,app_id,app_secret,password_sha256,is_active,fields_encrypted,key_version)
			VALUES ($1,$2,$3,$4,'','','',$5,$6,$7) RETURNING created_at`, a.ID, a.Vendor, a.Name, a.BaseURL, a.IsActive, sealed, r.box.Version()).Scan(&a.CreatedAt)
		a.HasCredentials = err == nil
		return err
	})
}

func (r *VendorAccountRepo) Update(ctx context.Context, a *types.VendorAccount) error {
	sealed, err := r.seal(*a)
	if err != nil {
		return err
	}
	return r.write(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE vendor_accounts SET name=$2,base_url=$3,is_active=$4,fields_encrypted=$5,key_version=$6 WHERE id=$1`,
			a.ID, a.Name, a.BaseURL, a.IsActive, sealed, r.box.Version())
		return err
	})
}

func (r *VendorAccountRepo) SetSyncResult(ctx context.Context, id int64, lastError string, t time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE vendor_accounts SET last_error=$2,last_sync_at=$3 WHERE id=$1`, id, lastError, t)
	return err
}

func (r *VendorAccountRepo) Delete(ctx context.Context, id int64) error {
	return r.write(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM vendor_accounts WHERE id=$1`, id)
		return err
	})
}
