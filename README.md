# Webpage Analysis Go

A Go web application that analyzes a webpage URL and reports useful HTML and HTTP details.

## Features

- `GET /`: Displays a form with a URL input and analyze button.
- `POST /`: Validates and analyzes the submitted URL.
- `POST /api/analyze`: API endpoint for URL analysis (JSON response).
- Reports:
  - HTML version
  - Page title
  - HTTP status code with color indicator (green for `200`, red for `4xx/5xx`)
  - Heading counts (`h1` to `h6`)
  - Internal and external links
  - Inaccessible links count
  - Login form detection
  - Additional response details (content type, content length, redirect count, response time, server header, final URL)
- Displays a useful error message with HTTP status code when URL parsing/fetching fails.

## Project Structure

- `main.go`: Server bootstrap and route setup.
- `config/config.go`: Environment-based configuration.
- `domain/web_analysis.go`: Core domain structs and associated methods for analysis response/error.
- `handlers/common.go`: Health endpoints.
- `handlers/webanalysis.go`: `WebAnalysisHandler`, analyzer interface, default HTTP analyzer implementation, and response rendering.
- `templates/*.gohtml`: Go templates split into layout, header, content, and footer.
- `static/js/script.js`: Client-side validation and submit button state.

## Configuration

Environment variables:

- `HTTP_LISTEN_ADDRESS` (default: `:8080`)
- `REQUEST_TIMEOUT_SECONDS` (default: `20`)
- `LINK_CHECK_TIMEOUT_SECONDS` (default: `5`)
- `MAX_CHECKED_LINKS` (default: `150`)

## Run Locally

```bash
go mod tidy
go run ./main.go
```

Open [http://localhost:8080](http://localhost:8080).

## Build Binary

```bash
go build -o webpage-analysis-go ./main.go
./webpage-analysis-go
```

## Run Tests

```bash
go test ./...
```

## API Usage (curl)

JSON request:

```bash
curl -X POST http://localhost:8080/api/analyze \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com"}'
```

Form request:

```bash
curl -X POST http://localhost:8080/api/analyze \
  -d "url=https://example.com"
```

## Docker

Build image:

```bash
docker build -t webpage-analysis-go .
```

Run container:

```bash
docker run --rm -p 8080:8080 webpage-analysis-go
```

Then open [http://localhost:8080](http://localhost:8080).
