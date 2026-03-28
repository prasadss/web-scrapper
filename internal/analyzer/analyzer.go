package analyzer

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"web-scrapper/internal/models"
	"web-scrapper/internal/security"
)

// URLValidator validates whether a URL is safe to fetch.
type URLValidator interface {
	ValidateURL(ctx context.Context, rawURL string) error
}

// Analyze takes the HTML body and base URL, returns the analysis result.
// The context is used to cancel in-flight link accessibility checks.
func Analyze(ctx context.Context, body io.Reader, baseURL *url.URL) *models.AnalysisResult {
	return AnalyzeWithValidator(ctx, body, baseURL, security.DefaultValidator())
}

// AnalyzeWithValidator takes the HTML body and base URL, using the provided
// validator for discovered-link safety checks and redirect validation.
func AnalyzeWithValidator(ctx context.Context, body io.Reader, baseURL *url.URL, validator URLValidator) *models.AnalysisResult {
	if validator == nil {
		validator = security.DefaultValidator()
	}

	result := &models.AnalysisResult{}

	headings := make(map[string]int)
	var links []string
	var insideTitle bool

	tokenizer := html.NewTokenizer(body)

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			if tokenizer.Err() == io.EOF {
				result.Headings = buildOrderedHeadings(headings)
				result.InternalLinks, result.ExternalLinks, result.InaccessibleLinks = classifyAndCheckLinks(ctx, links, baseURL, validator)
			}
			return result

		case html.DoctypeToken:
			token := tokenizer.Token()
			result.HTMLVersion = detectHTMLVersion(token.Data)

		case html.StartTagToken, html.SelfClosingTagToken:
			tn, hasAttr := tokenizer.TagName()
			tagName := string(tn)
			switch {
			case tagName == "title":
				insideTitle = true

			case isHeading(tagName):
				headings[tagName]++

			case tagName == "a" && hasAttr:
				if href := getAttr(tokenizer, "href"); href != "" {
					links = append(links, href)
				}

			case tagName == "input" && hasAttr:
				if strings.EqualFold(getAttr(tokenizer, "type"), "password") {
					result.HasLoginForm = true
				}
			}

		case html.TextToken:
			if insideTitle {
				result.Title += strings.TrimSpace(tokenizer.Token().Data)
			}

		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			if string(tn) == "title" {
				insideTitle = false
			}
		}
	}
}

// buildOrderedHeadings converts the heading map to an ordered slice (h1 through h6).
func buildOrderedHeadings(headings map[string]int) []models.HeadingCount {
	levels := []string{"h1", "h2", "h3", "h4", "h5", "h6"}
	var result []models.HeadingCount
	for _, level := range levels {
		if count, ok := headings[level]; ok {
			result = append(result, models.HeadingCount{Level: level, Count: count})
		}
	}
	return result
}

// classifyAndCheckLinks resolves, classifies, and checks accessibility of links.
func classifyAndCheckLinks(ctx context.Context, links []string, baseURL *url.URL, validator URLValidator) (internal, external, inaccessible int) {
	if validator == nil {
		validator = security.DefaultValidator()
	}

	seen := make(map[string]bool, len(links))
	safeURLs := make([]string, 0, len(links))

	for _, link := range links {
		parsedLink, err := url.Parse(link)
		if err != nil {
			continue
		}

		// Skip non-http(s) links (mailto:, javascript:, tel:, etc.)
		if parsedLink.Scheme != "" && parsedLink.Scheme != "http" && parsedLink.Scheme != "https" {
			continue
		}

		resolvedLink := baseURL.ResolveReference(parsedLink)
		resolvedLinkStr := resolvedLink.String()

		if seen[resolvedLinkStr] {
			continue
		}
		seen[resolvedLinkStr] = true

		// Check validator first — blocked links should not count as internal/external.
		if err := validator.ValidateURL(ctx, resolvedLinkStr); err != nil {
			inaccessible++
			continue
		}

		if strings.EqualFold(resolvedLink.Hostname(), baseURL.Hostname()) {
			internal++
		} else {
			external++
		}

		safeURLs = append(safeURLs, resolvedLinkStr)
	}

	inaccessible += checkAccessibility(ctx, safeURLs, validator)
	return internal, external, inaccessible
}

const _maxWorkers = 10

// checkAccessibility uses a fixed worker pool to check if links are reachable.
func checkAccessibility(ctx context.Context, urls []string, validator URLValidator) int {
	if len(urls) == 0 {
		return 0
	}

	if validator == nil {
		validator = security.DefaultValidator()
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: _maxWorkers,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return validator.ValidateURL(req.Context(), req.URL.String())
		},
	}

	work := make(chan string)
	go func() {
		defer close(work)
		for _, u := range urls {
			select {
			case work <- u:
			case <-ctx.Done():
				return
			}
		}
	}()

	var (
		mu           sync.Mutex
		wg           sync.WaitGroup
		inaccessible int
	)

	workers := _maxWorkers
	if len(urls) < workers {
		workers = len(urls)
	}

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for link := range work {
				if !checkLink(ctx, client, link) {
					mu.Lock()
					inaccessible++
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()
	return inaccessible
}

// checkLink performs a HEAD request (falling back to GET) and returns true if accessible.
func checkLink(ctx context.Context, client *http.Client, link string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, link, nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// Retry with GET if HEAD is not allowed
	if resp.StatusCode == http.StatusMethodNotAllowed {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
		if err != nil {
			return false
		}
		resp, err = client.Do(req)
		if err != nil {
			return false
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	return resp.StatusCode < 400
}

// detectHTMLVersion determines the HTML version from the DOCTYPE token data.
func detectHTMLVersion(data string) string {
	lower := strings.ToLower(strings.TrimSpace(data))

	if lower == "" {
		return ""
	}

	if lower == "html" {
		return "HTML5"
	}

	switch {
	case strings.Contains(lower, "xhtml 1.0"):
		return "XHTML 1.0"
	case strings.Contains(lower, "xhtml 1.1"):
		return "XHTML 1.1"
	case strings.Contains(lower, "html 4.01"):
		return "HTML 4.01"
	case strings.Contains(lower, "html 4.0"):
		return "HTML 4.0"
	case strings.Contains(lower, "html 3.2"):
		return "HTML 3.2"
	case strings.Contains(lower, "html 2.0"):
		return "HTML 2.0"
	default:
		return "Unknown"
	}
}

// isHeading checks if the tag name is h1 through h6.
func isHeading(tag string) bool {
	switch tag {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return true
	}
	return false
}

// getAttr retrieves an attribute value from the current token.
func getAttr(tokenizer *html.Tokenizer, key string) string {
	for {
		attrKey, attrVal, more := tokenizer.TagAttr()
		if string(attrKey) == key {
			return string(attrVal)
		}
		if !more {
			break
		}
	}
	return ""
}
