package api

import (
	"errors"
	"strings"

	"ata-energo/middleware"
	"ata-energo/types"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type stockItemInput struct {
	SKU       string  `json:"sku"`
	Name      string  `json:"name"`
	Category  string  `json:"category"`
	Unit      string  `json:"unit"`
	MinQty    float64 `json:"min_qty"`
	BuyPrice  float64 `json:"buy_price"`
	SellPrice float64 `json:"sell_price"`
	Location  string  `json:"location"`
}

type stockMovementInput struct {
	ItemID    int64    `json:"item_id"`
	Kind      string   `json:"kind"`
	Quantity  float64  `json:"quantity"`
	Reason    string   `json:"reason"`
	Note      string   `json:"note"`
	UnitPrice *float64 `json:"unit_price"`
}

func registerStock(router fiber.Router, pool *pgxpool.Pool) {
	stock := router.Group("/stock", func(c *fiber.Ctx) error {
		if middleware.GetUser(c).Role != types.RoleAdmin {
			return fiber.ErrForbidden
		}
		c.Set("Cache-Control", "no-store")
		return c.Next()
	})
	stock.Get("/items", func(c *fiber.Ctx) error {
		q := "%" + strings.TrimSpace(c.Query("q")) + "%"
		return crmJSON(c, pool, `SELECT coalesce(jsonb_agg(to_jsonb(i) ORDER BY i.name),'[]'::jsonb) FROM stock_items i WHERE (i.name ILIKE $1 OR i.sku ILIKE $1) AND ($2='true' OR NOT i.archived)`, q, c.Query("archived", "false"))
	})
	stock.Post("/items", func(c *fiber.Ctx) error {
		var in stockItemInput
		if err := c.BodyParser(&in); err != nil || strings.TrimSpace(in.SKU) == "" || strings.TrimSpace(in.Name) == "" || in.MinQty < 0 || in.BuyPrice < 0 || in.SellPrice < 0 {
			return detail(c, fiber.StatusBadRequest, "Проверьте данные позиции.")
		}
		if in.Unit == "" {
			in.Unit = "шт"
		}
		var id int64
		err := pool.QueryRow(c.UserContext(), `INSERT INTO stock_items(sku,name,category,unit,min_qty,buy_price,sell_price,location) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, strings.TrimSpace(in.SKU), strings.TrimSpace(in.Name), in.Category, in.Unit, in.MinQty, in.BuyPrice, in.SellPrice, in.Location).Scan(&id)
		if err != nil {
			return err
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"id": id})
	})
	stock.Patch("/items/:id", func(c *fiber.Ctx) error {
		var in stockItemInput
		if err := c.BodyParser(&in); err != nil {
			return detail(c, fiber.StatusBadRequest, "Некорректные данные.")
		}
		_, err := pool.Exec(c.UserContext(), `UPDATE stock_items SET sku=$2,name=$3,category=$4,unit=$5,min_qty=$6,buy_price=$7,sell_price=$8,location=$9,updated_at=now() WHERE id=$1`, c.Params("id"), in.SKU, in.Name, in.Category, in.Unit, in.MinQty, in.BuyPrice, in.SellPrice, in.Location)
		return err
	})
	stock.Get("/movements", func(c *fiber.Ctx) error {
		return crmJSON(c, pool, `SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY m.created_at DESC),'[]'::jsonb) FROM (SELECT sm.*,si.name item_name,si.sku,trim(u.first_name || ' ' || u.last_name) user_name FROM stock_movements sm JOIN stock_items si ON si.id=sm.item_id JOIN users u ON u.id=sm.user_id ORDER BY sm.created_at DESC LIMIT 500) m`)
	})
	stock.Post("/movements", func(c *fiber.Ctx) error {
		var in stockMovementInput
		if err := c.BodyParser(&in); err != nil || in.ItemID < 1 || in.Quantity <= 0 || strings.TrimSpace(in.Reason) == "" || (in.Kind != "in" && in.Kind != "out" && in.Kind != "adjust") {
			return detail(c, fiber.StatusBadRequest, "Проверьте данные движения.")
		}
		tx, err := pool.Begin(c.UserContext())
		if err != nil {
			return err
		}
		defer tx.Rollback(c.UserContext())
		var qty float64
		err = tx.QueryRow(c.UserContext(), `SELECT qty FROM stock_items WHERE id=$1 FOR UPDATE`, in.ItemID).Scan(&qty)
		if errors.Is(err, pgx.ErrNoRows) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return err
		}
		delta := in.Quantity
		if in.Kind == "out" {
			delta = -delta
		}
		if in.Kind == "adjust" {
			delta = in.Quantity - qty
		}
		after := qty + delta
		if after < 0 {
			return detail(c, fiber.StatusConflict, "Недостаточно на складе.")
		}
		if delta == 0 {
			return detail(c, fiber.StatusBadRequest, "Количество не изменилось.")
		}
		if _, err = tx.Exec(c.UserContext(), `INSERT INTO stock_movements(item_id,kind,qty_delta,qty_after,unit_price,reason,note,user_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, in.ItemID, in.Kind, delta, after, in.UnitPrice, in.Reason, in.Note, middleware.GetUser(c).ID); err != nil {
			return err
		}
		if _, err = tx.Exec(c.UserContext(), `UPDATE stock_items SET qty=$2,updated_at=now() WHERE id=$1`, in.ItemID, after); err != nil {
			return err
		}
		return tx.Commit(c.UserContext())
	})
}
