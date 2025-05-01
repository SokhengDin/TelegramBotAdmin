# Telegram Notification Service

A simple Go microservice that forwards JSON messages to Telegram.

## Features

- Send messages to Telegram with a simple API call
- Secure API with Bearer token authentication
- Formats JSON data in monospace code blocks
- Built with Fiber for high performance
- Docker ready

## Quick Start

### Environment Variables

Create a `.env` file with:

```
TELEGRAM_TOKEN=your_telegram_bot_token
TELEGRAM_CHAT_ID=your_telegram_chat_id
API_KEY=your_api_key_for_authentication
PORT=3000
```

### Run with Docker

```bash
docker-compose up -d
```

### Run without Docker

```bash
go mod download
go run main.go
```

## API Usage

Send a message to Telegram:

```bash
curl -X POST http://localhost:3000/api/v1/telegram/send \
  -H "Authorization: Bearer your_api_key" \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello from API", "timestamp": "2025-05-01"}'
```

## Health Check

```bash
curl http://localhost:3000/health
```

## License

MIT