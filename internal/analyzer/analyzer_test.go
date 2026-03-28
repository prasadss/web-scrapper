package analyzer

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"

	"web-scrapper/internal/security"
)

type fakeResolver struct {
	hosts map[string][]net.IP
}

func (r fakeResolver) LookupIP(_ context.Context, _ string, host string) ([]net.IP, error) {
	ips, ok := r.hosts[host]
	if !ok {
		return nil, fmt.Errorf("host not found: %s", host)
	}
	return ips, nil
}

func testValidator() *security.Validator {
	return security.NewValidator(fakeResolver{
		hosts: map[string][]net.IP{
			"example.com": {net.ParseIP("93.184.216.34")},
			"google.com":  {net.ParseIP("8.8.8.8")},
			"github.com":  {net.ParseIP("140.82.121.4")},
		},
	})
}

func TestDetectHTMLVersion(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		want string
	}{
		{
			name:     "HTML5",
			html:     `<!DOCTYPE html><html><head></head><body></body></html>`,
			want: "HTML5",
		},
		{
			name:     "HTML 4.01",
			html:     `<!DOCTYPE HTML PUBLIC "-//W3C//DTD HTML 4.01//EN" "http://www.w3.org/TR/html4/strict.dtd"><html><head></head><body></body></html>`,
			want: "HTML 4.01",
		},
		{
			name:     "XHTML 1.0",
			html:     `<!DOCTYPE html PUBLIC "-//W3C//DTD XHTML 1.0 Strict//EN" "http://www.w3.org/TR/xhtml1/DTD/xhtml1-strict.dtd"><html><head></head><body></body></html>`,
			want: "XHTML 1.0",
		},
		{
			name:     "No DOCTYPE",
			html:     `<html><head></head><body></body></html>`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseURL, _ := url.Parse("https://example.com")
			result := AnalyzeWithValidator(context.Background(), strings.NewReader(tt.html), baseURL, testValidator())
			if result.HTMLVersion != tt.want {
				t.Errorf("want %q, got %q", tt.want, result.HTMLVersion)
			}
		})
	}
}

func TestTitle(t *testing.T) {
	html := `<!DOCTYPE html><html><head><title>My Test Page</title></head><body></body></html>`
	baseURL, _ := url.Parse("https://example.com")
	result := AnalyzeWithValidator(context.Background(), strings.NewReader(html), baseURL, testValidator())

	if result.Title != "My Test Page" {
		t.Errorf("want %q, got %q", "My Test Page", result.Title)
	}
}

func TestHeadingsCount(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
		<h1>Title</h1>
		<h2>Sub 1</h2>
		<h2>Sub 2</h2>
		<h3>Detail</h3>
		<h3>Detail</h3>
		<h3>Detail</h3>
	</body></html>`

	baseURL, _ := url.Parse("https://example.com")
	result := AnalyzeWithValidator(context.Background(), strings.NewReader(html), baseURL, testValidator())

	want := map[string]int{"h1": 1, "h2": 2, "h3": 3}
	for _, h := range result.Headings {
		if want[h.Level] != h.Count {
			t.Errorf("heading %s: want %d, got %d", h.Level, want[h.Level], h.Count)
		}
		delete(want, h.Level)
	}
	if len(want) > 0 {
		t.Errorf("missing headings: %v", want)
	}
}

func TestHeadingsOrder(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
		<h3>Detail</h3>
		<h1>Title</h1>
		<h2>Sub</h2>
	</body></html>`

	baseURL, _ := url.Parse("https://example.com")
	result := AnalyzeWithValidator(context.Background(), strings.NewReader(html), baseURL, testValidator())

	if len(result.Headings) != 3 {
		t.Fatalf("want 3 heading levels, got %d", len(result.Headings))
	}

	// Headings must always be ordered h1, h2, h3 regardless of document order
	wantOrder := []string{"h1", "h2", "h3"}
	for i, h := range result.Headings {
		if h.Level != wantOrder[i] {
			t.Errorf("heading[%d]: want %q, got %q", i, wantOrder[i], h.Level)
		}
	}
}

func TestLoginFormDetection(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		want bool
	}{
		{
			name:     "Has login form",
			html:     `<!DOCTYPE html><html><body><form><input type="text" name="user"><input type="password" name="pass"></form></body></html>`,
			want: true,
		},
		{
			name:     "No login form",
			html:     `<!DOCTYPE html><html><body><form><input type="text" name="search"><button>Search</button></form></body></html>`,
			want: false,
		},
		{
			name:     "Password type uppercase",
			html:     `<!DOCTYPE html><html><body><form><input type="PASSWORD" name="pass"></form></body></html>`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseURL, _ := url.Parse("https://example.com")
			result := AnalyzeWithValidator(context.Background(), strings.NewReader(tt.html), baseURL, testValidator())
			if result.HasLoginForm != tt.want {
				t.Errorf("want HasLoginForm=%v, got %v", tt.want, result.HasLoginForm)
			}
		})
	}
}

func TestLinkClassification(t *testing.T) {
	html := `<!DOCTYPE html><html><body>
		<a href="/about">About</a>
		<a href="/contact">Contact</a>
		<a href="https://example.com/blog">Blog</a>
		<a href="https://google.com">Google</a>
		<a href="https://github.com">GitHub</a>
		<a href="mailto:test@example.com">Email</a>
		<a href="javascript:void(0)">JS Link</a>
	</body></html>`

	baseURL, _ := url.Parse("https://example.com")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := AnalyzeWithValidator(ctx, strings.NewReader(html), baseURL, testValidator())

	// /about, /contact, /blog are internal (same host)
	if result.InternalLinks != 3 {
		t.Errorf("want 3 internal links, got %d", result.InternalLinks)
	}

	// google.com, github.com are external
	if result.ExternalLinks != 2 {
		t.Errorf("want 2 external links, got %d", result.ExternalLinks)
	}
}

func TestEmptyPage(t *testing.T) {
	html := ``
	baseURL, _ := url.Parse("https://example.com")
	result := AnalyzeWithValidator(context.Background(), strings.NewReader(html), baseURL, testValidator())

	if result.Title != "" {
		t.Errorf("want empty title, got %q", result.Title)
	}
	if result.HasLoginForm {
		t.Errorf("want no login form")
	}
	if len(result.Headings) != 0 {
		t.Errorf("want no headings")
	}
}

func TestClassifyAndCheckLinks_BlockedLinksCountAsInaccessible(t *testing.T) {
	validator := security.NewValidator(fakeResolver{
		hosts: map[string][]net.IP{
			"example.com": {net.ParseIP("93.184.216.34")},
		},
	})

	baseURL, _ := url.Parse("https://example.com")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	internal, external, inaccessible := classifyAndCheckLinks(
		ctx,
		[]string{
			"/about",
			"http://127.0.0.1/admin",
			"http://192.168.1.20/healthz",
		},
		baseURL,
		validator,
	)

	if internal != 1 {
		t.Fatalf("want 1 internal link, got %d", internal)
	}
	// Blocked links no longer count as external — they are only inaccessible.
	if external != 0 {
		t.Fatalf("want 0 external links (blocked links excluded), got %d", external)
	}
	if inaccessible != 2 {
		t.Fatalf("want 2 inaccessible links from security blocking, got %d", inaccessible)
	}
}

