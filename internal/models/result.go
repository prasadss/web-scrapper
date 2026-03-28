package models

// HeadingCount represents a heading level and its count.
type HeadingCount struct {
	Level string
	Count int
}

// AnalysisResult holds the output of analyzing a webpage.
type AnalysisResult struct {
	HTMLVersion       string
	Title             string
	Headings          []HeadingCount
	InternalLinks     int
	ExternalLinks     int
	InaccessibleLinks int
	HasLoginForm      bool
	RenderMode        string
}

// PageData is passed to the HTML template for rendering.
type PageData struct {
	URL        string
	Result     *AnalysisResult
	Error      string
	StatusCode int
}
