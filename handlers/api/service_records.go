package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"ata-energo/middleware"
	"ata-energo/types"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func registerServiceRecords(router fiber.Router, pool *pgxpool.Pool) {
	archive := router.Group("/service-records", func(c *fiber.Ctx) error {
		if middleware.GetUser(c).Role != types.RoleAdmin {
			return detail(c, fiber.StatusForbidden, "Доступно администратору.")
		}
		c.Set("Cache-Control", "no-store")
		return c.Next()
	})

	type collection struct {
		Key   string `json:"key"`
		Count int    `json:"count"`
	}
	archive.Get("/collections", func(c *fiber.Ctx) error {
		rows, err := pool.Query(c.UserContext(), `
			SELECT collection, count(*) FROM service_records GROUP BY collection ORDER BY collection`)
		if err != nil {
			return err
		}
		result, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (collection, error) {
			var item collection
			err := row.Scan(&item.Key, &item.Count)
			return item, err
		})
		if err != nil {
			return err
		}
		return c.JSON(result)
	})

	type record struct {
		ID   string          `json:"id"`
		Data json.RawMessage `json:"data"`
	}
	archive.Get("/rows", func(c *fiber.Ctx) error {
		offset := c.QueryInt("offset", 0)
		if offset < 0 {
			return detail(c, fiber.StatusBadRequest, "Некорректная позиция.")
		}
		rows, err := pool.Query(c.UserContext(), `
			SELECT source_id, data - ARRAY['pass_hash','pw','token','csrf','share_token']
			FROM service_records WHERE collection=$1
			ORDER BY length(source_id), source_id LIMIT 25 OFFSET $2`, c.Query("collection"), offset)
		if err != nil {
			return err
		}
		result, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (record, error) {
			var item record
			err := row.Scan(&item.ID, &item.Data)
			return item, err
		})
		if err != nil {
			return err
		}
		return c.JSON(result)
	})

	archive.Get("/photo", func(c *fiber.Ctx) error {
		var content []byte
		err := pool.QueryRow(c.UserContext(), `
			SELECT content FROM service_record_files WHERE path=$1`, c.Query("path")).Scan(&content)
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound(c)
		}
		if err != nil {
			return err
		}
		mime := http.DetectContentType(content)
		switch mime {
		case "image/jpeg", "image/png", "image/gif", "image/webp":
			c.Set("Content-Type", mime)
		default:
			return notFound(c)
		}
		c.Set("X-Content-Type-Options", "nosniff")
		return c.Send(content)
	})
}
