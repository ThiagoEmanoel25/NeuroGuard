package handler

import (
	"errors"

	"github.com/gofiber/fiber/v2"
)

// ErrorHandler converte erros HTTP em uma resposta JSON estável para os clientes.
// Erros inesperados não expõem detalhes internos do servidor.
func ErrorHandler(c *fiber.Ctx, err error) error {
	status := fiber.StatusInternalServerError
	message := "erro interno do servidor"

	var fiberErr *fiber.Error
	if errors.As(err, &fiberErr) {
		status = fiberErr.Code
		message = fiberErr.Message
	}

	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{
			"code":    status,
			"message": message,
		},
	})
}
