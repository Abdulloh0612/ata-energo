package api

import (
	"strconv"

	"ata-energo/middleware"
	"ata-energo/services"

	"github.com/gofiber/fiber/v2"
)

func RegisterTelegram(router fiber.Router, bot *services.Telegram, jwt fiber.Handler) {
	if bot == nil {
		return
	}
	router.Post("/auth/telegram", func(c *fiber.Ctx) error {
		var req struct {
			InitData string `json:"init_data"`
		}
		if err := parseBody(c, &req); err != nil {
			return err
		}
		result, err := bot.Login(c.UserContext(), req.InitData)
		if err != nil {
			return fail(c, err)
		}
		c.Set("Cache-Control", "no-store")
		return c.JSON(result)
	})
	router.Get("/me/telegram", jwt, func(c *fiber.Ctx) error {
		linked, err := bot.Linked(c.UserContext(), middleware.GetUser(c).ID)
		if err != nil {
			return fail(c, err)
		}
		return c.JSON(linked)
	})
	router.Patch("/me/telegram", jwt, func(c *fiber.Ctx) error {
		var req services.TelegramPreferences
		if err := parseBody(c, &req); err != nil {
			return err
		}
		settings, err := bot.SetPreferences(c.UserContext(), middleware.GetUser(c).ID, req)
		if err != nil {
			return fail(c, err)
		}
		return c.JSON(settings)
	})
	router.Post("/alerts/:id/response", jwt, func(c *fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil || id <= 0 {
			return fiber.ErrNotFound
		}
		var req struct {
			Status string `json:"status"`
		}
		if err := parseBody(c, &req); err != nil {
			return err
		}
		if err := bot.ChangeIncident(c.UserContext(), middleware.GetUser(c).ID, id, req.Status); err != nil {
			return fail(c, err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
	router.Post("/me/telegram/link", jwt, func(c *fiber.Ctx) error {
		link, err := bot.Link(c.UserContext(), middleware.GetUser(c).ID)
		if err != nil {
			return fail(c, err)
		}
		c.Set("Cache-Control", "no-store")
		return c.JSON(fiber.Map{"url": link})
	})
}
