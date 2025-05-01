package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Config struct {
	TelegramToken  string
	TelegramChatID int64
	APIKey         string
	Port           string
	Prefork        bool
	Concurrency    int
}

func main() {

	config, err := loadConfig()
	if err != nil {
		log.Fatal("Failed to load configuration: ", err)
	}

	bot, err := tgbotapi.NewBotAPI(config.TelegramToken)
	if err != nil {
		log.Fatalf("Failed to initialize bot: %v", err)
	}
	log.Printf("Authorized on account %s", bot.Self.UserName)

	app := fiber.New(fiber.Config{
		ErrorHandler:          customErrorHandler,
		Prefork:               config.Prefork,
		Concurrency:           config.Concurrency,
		DisableStartupMessage: true,
		ReadTimeout:           5 * time.Second,
		WriteTimeout:          10 * time.Second,
		IdleTimeout:           120 * time.Second,
	})

	app.Use(recover.New())
	app.Use(logger.New())

	app.Use("/api", authMiddleware(config.APIKey))

	app.Post("/api/v1/telegram/send", func(c *fiber.Ctx) error {

		var data map[string]interface{}
		if err := json.Unmarshal(c.Body(), &data); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "Invalid JSON")
		}

		messageVal, ok := data["message"]
		if !ok {
			return fiber.NewError(fiber.StatusBadRequest, "Missing 'message' field")
		}

		_, ok = messageVal.(string)
		if !ok {
			return fiber.NewError(fiber.StatusBadRequest, "'message' field must be a string")
		}

		resultChan := make(chan error, 1)

		go func() {

			formattedMessage := formatMessageWithoutBraces(data)

			msg := tgbotapi.NewMessage(config.TelegramChatID, formattedMessage)
			msg.ParseMode = "HTML"

			_, err := bot.Send(msg)
			resultChan <- err
		}()

		select {
		case err := <-resultChan:
			if err != nil {
				log.Printf("Failed to send Telegram message: %v", err)
				return fiber.NewError(fiber.StatusInternalServerError, "Failed to send message")
			}
			return c.JSON(fiber.Map{
				"success": true,
				"message": "Message sent successfully",
			})
		case <-time.After(10 * time.Second):
			return fiber.NewError(fiber.StatusGatewayTimeout, "Telegram API timeout")
		}
	})

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	go gracefulShutdown(app)

	log.Printf("Server starting on port %s with concurrency %d", config.Port, config.Concurrency)
	if err := app.Listen(":" + config.Port); err != nil {
		log.Fatal("Server failed to start: ", err)
	}
}

func loadConfig() (*Config, error) {

	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: .env file not found, using environment variables")
	}

	token := os.Getenv("TELEGRAM_TOKEN")
	chatIDStr := os.Getenv("TELEGRAM_CHAT_ID")
	apiKey := os.Getenv("API_KEY")
	port := os.Getenv("PORT")
	preforkStr := os.Getenv("PREFORK")
	concurrencyStr := os.Getenv("CONCURRENCY")

	if token == "" || chatIDStr == "" || apiKey == "" {
		return nil, fmt.Errorf("TELEGRAM_TOKEN, TELEGRAM_CHAT_ID and API_KEY must be set")
	}

	if port == "" {
		port = "3005"
	}

	chatIDStr = strings.TrimSpace(chatIDStr)
	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {

		log.Printf("Raw TELEGRAM_CHAT_ID value: '%s'", chatIDStr)
		return nil, fmt.Errorf("invalid TELEGRAM_CHAT_ID: %v", err)
	}

	prefork := false
	if preforkStr == "true" || preforkStr == "1" {
		prefork = true
	}

	concurrency := 256 * 1024
	if concurrencyStr != "" {
		if parsed, err := strconv.Atoi(concurrencyStr); err == nil && parsed > 0 {
			concurrency = parsed
		}
	} else {

		cpus := runtime.NumCPU()
		if cpus > 1 {
			concurrency = 256 * 1024 * cpus
		}
	}

	return &Config{
		TelegramToken:  token,
		TelegramChatID: chatID,
		APIKey:         apiKey,
		Port:           port,
		Prefork:        prefork,
		Concurrency:    concurrency,
	}, nil
}

func formatMessageWithoutBraces(data map[string]interface{}) string {
	var builder strings.Builder
	builder.WriteString("<code>")

	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := data[key]

		var valueStr string
		switch v := value.(type) {
		case string:
			valueStr = v
		case nil:
			valueStr = "null"
		default:
			valueBytes, err := json.Marshal(v)
			if err != nil {
				valueStr = fmt.Sprintf("%v", v)
			} else {
				valueStr = string(valueBytes)
			}
		}

		builder.WriteString(fmt.Sprintf("%s: %s\n", key, valueStr))
	}

	builder.WriteString("</code>")
	return builder.String()
}

func customErrorHandler(c *fiber.Ctx, err error) error {

	code := fiber.StatusInternalServerError

	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}

	return c.Status(code).JSON(fiber.Map{
		"success": false,
		"message": err.Error(),
	})
}

func authMiddleware(apiKey string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		auth := c.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != apiKey {
			return fiber.NewError(fiber.StatusUnauthorized, "Unauthorized")
		}
		return c.Next()
	}
}

func gracefulShutdown(app *fiber.App) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(ctx); err != nil {
		log.Fatal("Server forced to shutdown: ", err)
	}

	log.Println("Server gracefully stopped")
}
