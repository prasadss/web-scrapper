# Web Page Analyzer

A production-grade web application built in Go that analyzes the structure and content of any given webpage URL, including JavaScript-rendered Single Page Applications.

## Features

- **SPA support** — Uses Rod (headless browser) to render JavaScript before analysis, handling React/Angular/Vue apps
- Detects HTML version (HTML5, HTML 4.01, XHTML 1.0/1.1, HTML 3.2, HTML 2.0)
- Preserves the live page DOCTYPE from `document.doctype` before analysis, so HTML version detection still works after browser rendering
- Extracts page title
- Counts headings by level (h1–h6) in deterministic order
- Classifies links as internal or external (with relative URL resolution)
- Checks link accessibility via concurrent HEAD requests (with GET fallback on 405)
- Detects login forms (password input fields)
- Handles redirects correctly — uses final URL after redirect chain for link classification
- Skips non-HTTP links (mailto:, javascript:, tel:)
- Deduplicates links before counting and accessibility checks
- Displays meaningful error messages with HTTP status codes for unreachable URLs
- SSRF protection — blocks private IPs, cloud metadata endpoints, and dangerous URL patterns
- Request-scoped structured logging with unique request IDs (JSON via `slog`)
- In-memory result caching with TTL (5 min) to deduplicate repeated requests
- Application metrics exposed at `/metrics` (total requests, success/fail counts, cache hits)
- Graceful shutdown on SIGINT/SIGTERM

## Quick Start (Docker)

The fastest way to run the application:

```bash
docker-compose up --build
```

Open [http://localhost:8080](http://localhost:8080) in your browser. That's it.

To stop:

```bash
docker-compose down
```

## Manual Build & Run

### Prerequisites

- [Go 1.26+](https://go.dev/dl/)
- A Chromium or Chrome-compatible browser installed locally and available on `PATH`

```bash
# Clone the repository
git clone <repository-url>
cd web-scrapper

# Download dependencies
go mod download

# Build
make build

# Run
make run
```

The server starts at [http://localhost:8080](http://localhost:8080). Default port is `8080`.

To use a different port:

```bash
PORT=3000 make run
```

**Note:** The binary must be run from the project root directory, as template and static file paths are resolved relative to the working directory.

**Browser note:** The app currently uses `launcher.LookPath()` to find an installed browser. Docker works out of the box because the runtime image installs `chromium`, but local development requires Chrome/Chromium to already be installed.

## Other Commands

```bash
make test    # Run all tests
make vet     # Run static analysis
make fmt     # Format code
make clean   # Remove build artifacts
```

## Project Structure

```
web-scrapper/
├── cmd/server/              # Application entry point (dependency init + server lifecycle)
│   └── main.go
├── internal/
│   ├── analyzer/            # Core HTML analysis logic
│   │   ├── analyzer.go
│   │   └── analyzer_test.go
│   ├── cache/               # In-memory TTL result cache
│   │   └── cache.go
│   ├── controller/          # Business logic handlers
│   │   └── analyzer.go
│   ├── middleware/           # Request ID, structured logging, metrics
│   │   ├── requestid.go
│   │   ├── logging.go
│   │   └── metrics.go
│   ├── models/              # Shared data structures
│   │   └── result.go
│   ├── renderer/            # Headless browser rendering (Rod)
│   │   ├── renderer.go
│   │   ├── renderer_test.go
│   │   └── request_guard.go
│   ├── security/            # SSRF protection + URL blocklist
│   │   ├── security.go
│   │   └── security_test.go
│   └── transport/           # Route definitions + middleware wiring
│       └── router.go
├── web/
│   ├── templates/           # HTML templates
│   │   └── index.html
│   └── static/              # CSS files
│       └── style.css
├── Dockerfile               # Multi-stage Docker build
├── docker-compose.yml       # One-command setup
├── Makefile                 # Build automation
├── README.md
└── .gitignore
```

## Assumptions & Decisions

1. **JavaScript rendering via Rod** — The analyzer uses Rod (headless Chromium) to render pages before analysis. This ensures JavaScript-rendered SPAs (React, Angular, Vue) are fully analyzed, not just the empty shell HTML. See [Why Rod?](#why-rod) below.

2. **Login form detection** — A page is considered to contain a login form if it has an `<input type="password">` element. This is a reliable heuristic as virtually all login forms require a password field. Registration forms with password fields will also be flagged — an acceptable trade-off given the requirement's scope.

3. **Link classification** — Links are classified as internal or external by comparing the hostname of the resolved link against the hostname of the analyzed page (after following redirects). Subdomains (e.g., `blog.example.com` vs `example.com`) are treated as external since they may be entirely separate services.

4. **Redirect handling** — The final URL after all redirects is used as the base for link classification, not the originally submitted URL. This ensures correct internal/external classification when a site redirects (e.g., `http://example.com` → `https://www.example.com`).

5. **Link accessibility** — Links are checked using HTTP HEAD requests with a 5-second timeout per request. If a server returns 405 (Method Not Allowed), a GET request is used as a fallback. Links returning HTTP 4xx/5xx or network errors are counted as inaccessible. Duplicate URLs are checked only once.

6. **Concurrency** — Link accessibility checks run concurrently with a maximum of 10 simultaneous requests to avoid overwhelming target servers or exhausting local resources.

7. **URL input** — If a user enters a URL without a scheme (e.g., `example.com`), the application prepends `https://` as a secure default.

8. **Non-HTTP links** — Links with `mailto:`, `javascript:`, `tel:`, and other non-HTTP schemes are excluded from both classification and accessibility checks.

9. **HTML version detection** — Detection is based on the DOCTYPE declaration. Because browser-rendered DOM HTML often omits the original `<!DOCTYPE ...>` when serialized, the renderer reads `document.doctype` in the browser and prepends it to the HTML passed to the tokenizer. The analyzer then matches the DOCTYPE token data against known patterns for HTML5, HTML 4.01, XHTML 1.0/1.1, HTML 3.2, and HTML 2.0. Pages without a DOCTYPE are reported as having no detected version.

10. **SSRF protection** — All URLs are validated before fetching. Private/internal IP ranges (127.0.0.0/8, 10.0.0.0/8, 192.168.0.0/16, 172.16.0.0/12), cloud metadata endpoints (169.254.169.254), and dangerous URL patterns (admin panels, database ports) are blocked.

11. **Caching** — Analysis results are cached in-memory keyed by the final (post-redirect) URL with a 5-minute TTL. Repeat requests for the same URL return cached results instantly.

## Why Rod?

The requirement is to analyze webpage content, but modern websites increasingly rely on JavaScript to render their DOM. A simple `http.Get` only retrieves the raw HTML — for a React app, that's just `<div id="root"></div>`.

We evaluated four options:

| Tool | JS Rendering | Browser Handling In This Project | Pure Go | Extra Runtime |
|------|:-----------:|:-------------------------------:|:-------:|:-------------:|
| `net/http` | No | N/A | Yes | None |
| Colly | No | N/A | Yes | None |
| chromedp | Yes | Needs Chrome pre-installed | Yes | None |
| **Rod** | **Yes** | Uses installed Chrome/Chromium via `launcher.LookPath()` | **Yes** | **None** |
| playwright-go | Yes | Browser install managed separately | No (Go→Node.js bridge) | Node.js |

**Rod was chosen because:**

1. **Pure Go** — Talks directly to Chrome DevTools Protocol. No hidden Node.js runtime like playwright-go. Clean dependency for a Go project.
2. **Good fit for browser automation** — Rod's API (`WaitStable`, `WaitLoad`, `Eval`, request hijacking) maps well to rendering and scraping workflows.
3. **Simple Docker story** — The image installs `chromium`, and Rod connects to it directly. That keeps the runtime explicit and predictable.
4. **Designed for scraping** — Rod's API is better aligned with page rendering use cases than lower-level DevTools wiring.
5. **Lightweight** — Fewer transitive dependencies than playwright-go, simpler codebase than chromedp.

## Why The JS DOCTYPE Logic Exists

HTML version detection depends on seeing a `<!DOCTYPE ...>` token. That works when the analyzer processes raw HTML, but after a page is rendered in the browser, `page.HTML()` returns the DOM HTML and often drops the doctype entirely.

The browser still keeps the doctype in `document.doctype`, so the renderer reads it with JavaScript and prepends it back onto the serialized HTML before analysis. Without that step, JavaScript-rendered pages could have a valid doctype in the browser and still be reported as having no detected HTML version.

## Possible Improvements

### Observability

- **Prometheus metrics** — Replace the custom `/metrics` JSON endpoint with Prometheus-compatible metrics for integration with Grafana dashboards and alerting.
- **Distributed tracing** — Add OpenTelemetry spans for request tracing across the fetch → analyze → link-check pipeline.
- **Error breakdown** — Expose metrics by error type (DNS failure, timeout, HTTP 4xx/5xx, SSRF blocked).

### Security

- **Rate limiting** — Add per-IP request rate limiting on `/analyze` to prevent abuse in a public deployment.
- **Request body size limit** — Cap incoming POST body size to prevent abuse.

### Performance

- **Redis cache** — Replace in-memory cache with Redis for multi-instance deployments behind a load balancer.
- **Browser pool** — Maintain a pool of Rod browser tabs for concurrent rendering, reducing per-request overhead.
- **Link count cap** — Limit the number of links checked for accessibility (e.g., max 100) to prevent long analysis times on link-heavy pages.

### Deployment

- **Single binary** — Use Go's `embed` package to bundle templates and static files into the binary, eliminating the need to deploy the `web/` directory separately.
- **Health check endpoint** — Add `/healthz` for container orchestration (Kubernetes, ECS).

### Feature Enhancements

- **Depth-limited crawling** — Extend the analyzer to crawl and analyze multiple pages within a site.
- **Integration tests** — Add HTTP handler tests using `httptest.NewServer` covering error responses, redirects, and edge cases.
- **PDF export** — Allow users to download analysis results as a PDF report.
