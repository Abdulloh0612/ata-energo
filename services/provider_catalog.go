package services

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"ata-energo/pkg/deye"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CatalogSite struct {
	ExternalID, Name, Address, Timezone, Status string
	Latitude, Longitude                         *float64
}

type SiteCatalogAdapter interface {
	FetchSites(context.Context, int64) ([]CatalogSite, error)
}

type ProviderCatalog struct {
	pool     *pgxpool.Pool
	adapters map[string]SiteCatalogAdapter
	mu       sync.Mutex
}

func NewProviderCatalog(pool *pgxpool.Pool, adapters map[string]SiteCatalogAdapter) *ProviderCatalog {
	return &ProviderCatalog{pool: pool, adapters: adapters}
}

func (s *DeyeService) FetchSites(ctx context.Context, accountID int64) ([]CatalogSite, error) {
	_, client, err := s.Account(ctx, accountID)
	if err != nil {
		return nil, err
	}
	const size = 200
	out := make([]CatalogSite, 0)
	seen := make(map[int64]bool)
	for page := 1; page <= 500; page++ {
		reply, err := client.StationList(ctx, deye.PageRequest{Page: page, Size: size})
		if err != nil {
			return nil, err
		}
		for _, record := range reply.StationList {
			if record.ID <= 0 || seen[record.ID] {
				return nil, errors.New("Deye вернул повторяющуюся или некорректную станцию")
			}
			seen[record.ID] = true
			out = append(out, normalizeDeyeSite(record))
		}
		if reply.Total > 0 {
			if len(out) >= reply.Total {
				return out, nil
			}
		} else if len(reply.StationList) < size {
			return out, nil
		}
		if len(reply.StationList) == 0 {
			return nil, errors.New("Deye вернул неполный каталог")
		}
	}
	return nil, errors.New("превышен предел страниц каталога Deye")
}

func normalizeDeyeSite(site deye.Station) CatalogSite {
	id := strconv.FormatInt(site.ID, 10)
	name := strings.TrimSpace(site.Name)
	if name == "" {
		name = "Объект " + id
	}
	zone := strings.TrimSpace(site.RegionTimezone)
	if zone == "" || zone == "Local" {
		zone = "Asia/Bishkek"
	}
	if _, err := time.LoadLocation(zone); err != nil {
		zone = "Asia/Bishkek"
	}
	state := map[string]string{"NORMAL": "normal", "PARTIAL_OFFLINE": "warning", "ALL_OFFLINE": "offline", "NO_DEVICE": "empty"}[site.ConnectionStatus]
	if state == "" {
		state = "unknown"
	}
	lat, lng := site.LocationLat, site.LocationLng
	if lat != nil && (*lat < -90 || *lat > 90) {
		lat = nil
	}
	if lng != nil && (*lng < -180 || *lng > 180) {
		lng = nil
	}
	return CatalogSite{ExternalID: id, Name: name, Address: site.LocationAddress, Timezone: zone, Status: state, Latitude: lat, Longitude: lng}
}

// Sync serializes catalog imports in this process and records a result per account.
func (c *ProviderCatalog) Sync(ctx context.Context, accountID int64) error {
	if !c.mu.TryLock() {
		return Detail(409, "Синхронизация каталога уже выполняется")
	}
	defer c.mu.Unlock()
	var provider, adapter string
	err := c.pool.QueryRow(ctx, `SELECT a.vendor,p.adapter FROM vendor_accounts a JOIN providers p ON p.code=a.vendor
		WHERE a.id=$1 AND a.is_active AND p.enabled AND NOT p.mock`, accountID).Scan(&provider, &adapter)
	if err != nil {
		return Detail(404, "Активный облачный аккаунт не найден")
	}
	driver := c.adapters[adapter]
	if driver == nil {
		return Detail(400, "Для провайдера недоступна синхронизация каталога")
	}
	sites, syncErr := driver.FetchSites(ctx, accountID)
	if syncErr == nil {
		syncErr = c.persist(ctx, provider, accountID, sites)
	}
	var message string
	if syncErr != nil {
		message = "Не удалось обновить каталог провайдера"
	}
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err = c.pool.Exec(recordCtx, `INSERT INTO provider_catalog_sync(account_id,last_attempt_at,last_success_at,last_error)
		VALUES ($1,now(),CASE WHEN $2='' THEN now() END,$2)
		ON CONFLICT(account_id) DO UPDATE SET last_attempt_at=excluded.last_attempt_at,
		last_success_at=coalesce(excluded.last_success_at,provider_catalog_sync.last_success_at),last_error=excluded.last_error`, accountID, message)
	if syncErr != nil {
		return Detail(502, message)
	}
	return err
}

func (c *ProviderCatalog) persist(ctx context.Context, provider string, accountID int64, sites []CatalogSite) error {
	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ids := make([]string, 0, len(sites))
	for _, site := range sites {
		ids = append(ids, site.ExternalID)
		_, err = tx.Exec(ctx, `INSERT INTO provider_sites(provider_code,credential_id,external_id,name,address,timezone,status,latitude,longitude)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT(provider_code,credential_id,external_id) DO UPDATE SET archived=false,
			name=excluded.name,address=excluded.address,timezone=excluded.timezone,status=excluded.status,
			latitude=excluded.latitude,longitude=excluded.longitude,last_synced_at=now()`, provider, accountID, site.ExternalID, site.Name, site.Address, site.Timezone, site.Status, site.Latitude, site.Longitude)
		if err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE provider_sites SET archived=true WHERE provider_code=$1 AND credential_id=$2 AND NOT archived AND NOT(external_id=ANY($3))`, provider, accountID, ids); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (c *ProviderCatalog) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		c.poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *ProviderCatalog) poll(ctx context.Context) {
	rows, err := c.pool.Query(ctx, `SELECT a.id FROM vendor_accounts a JOIN providers p ON p.code=a.vendor WHERE a.is_active AND p.enabled AND NOT p.mock ORDER BY a.id`)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		work, cancel := context.WithTimeout(ctx, 90*time.Second)
		_ = c.Sync(work, id)
		cancel()
	}
}

func DemoEybondSites() []CatalogSite {
	result := make([]CatalogSite, 20)
	for i := range result {
		result[i] = CatalogSite{ExternalID: fmt.Sprintf("demo-eybond-%02d", i+1), Name: fmt.Sprintf("Солнечный объект Ош · %02d", i+1),
			Address: "Ош", Timezone: "Asia/Bishkek", Status: []string{"normal", "warning", "offline", "empty"}[i%4]}
	}
	return result
}
