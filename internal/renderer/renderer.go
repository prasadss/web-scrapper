package renderer

import (
	"context"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"web-scrapper/internal/security"
)

const documentDoctypeJS = `() => document.doctype ? new XMLSerializer().serializeToString(document.doctype) : ""`

// Renderer uses a headless browser to render JavaScript-heavy pages.
type Renderer struct {
	browser *rod.Browser
	timeout time.Duration
	guard   *requestGuard
}

// New creates a Renderer backed by a persistent headless Chrome process.
// Rod auto-downloads Chromium if not found locally.
func New(timeout time.Duration, validator URLValidator) (*Renderer, error) {
	if validator == nil {
		validator = security.DefaultValidator()
	}

	path, ok := launcher.LookPath()
	if !ok {
		return nil, fmt.Errorf("look up browser path: browser not found")
	}

	u, err := launcher.New().
		Bin(path).
		NoSandbox(true).
		Headless(true).
		Set("disable-dev-shm-usage").
		Launch()
	if err != nil {
		return nil, fmt.Errorf("launch browser: %w", err)
	}

	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("connect to browser: %w", err)
	}

	return &Renderer{
		browser: browser,
		timeout: timeout,
		guard:   newRequestGuard(validator),
	}, nil
}

// RenderPage navigates to the URL, waits for the page to stabilize,
// and returns the fully rendered HTML.
func (r *Renderer) RenderPage(ctx context.Context, rawURL string) (string, string, error) {
	page, err := r.browser.Page(proto.TargetCreateTarget{URL: ""})
	if err != nil {
		return "", "", fmt.Errorf("create page: %w", err)
	}
	defer page.Close()

	page = page.Context(ctx).Timeout(r.timeout)

	guardSession, err := r.guard.attach(page)
	if err != nil {
		return "", "", fmt.Errorf("start request guard: %w", err)
	}
	defer func() { _ = guardSession.close() }()

	if err := page.Navigate(rawURL); err != nil {
		if blockedErr := guardSession.blockedErr(); blockedErr != nil {
			return "", "", blockedErr
		}
		return "", "", fmt.Errorf("navigate: %w", err)
	}

	if err := page.WaitLoad(); err != nil {
		if blockedErr := guardSession.blockedErr(); blockedErr != nil {
			return "", "", blockedErr
		}
		return "", "", fmt.Errorf("wait for page load: %w", err)
	}

	// Allow JS frameworks (React, Vue, Angular) to finish rendering
	if err := page.WaitStable(500 * time.Millisecond); err != nil {
		if blockedErr := guardSession.blockedErr(); blockedErr != nil {
			return "", "", blockedErr
		}
		return "", "", fmt.Errorf("wait for page stability: %w", err)
	}

	// Get the final URL after any redirects
	info, err := page.Info()
	if err != nil {
		return "", "", fmt.Errorf("get page info: %w", err)
	}
	finalURL := info.URL

	// Extract the fully rendered HTML from the DOM
	html, err := page.HTML()
	if err != nil {
		return "", "", fmt.Errorf("get HTML: %w", err)
	}

	doctype, err := getDocumentDoctype(page)
	if err != nil {
		return "", "", fmt.Errorf("get doctype: %w", err)
	}

	return prependDoctype(doctype, html), finalURL, nil
}

// Close shuts down the headless browser.
func (r *Renderer) Close() {
	if r.browser != nil {
		r.browser.Close()
	}
}

func getDocumentDoctype(page *rod.Page) (string, error) {
	res, err := page.Eval(documentDoctypeJS)
	if err != nil {
		return "", err
	}
	return res.Value.Str(), nil
}

func prependDoctype(doctype, html string) string {
	if doctype == "" {
		return html
	}
	return doctype + html
}
