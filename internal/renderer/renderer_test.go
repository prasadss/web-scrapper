package renderer

import "testing"

func TestPrependDoctype(t *testing.T) {
	tests := []struct {
		name    string
		doctype string
		html    string
		want    string
	}{
		{
			name:    "doctype present",
			doctype: "<!DOCTYPE html>",
			html:    "<html><body>ok</body></html>",
			want:    "<!DOCTYPE html><html><body>ok</body></html>",
		},
		{
			name:    "doctype missing",
			doctype: "",
			html:    "<html><body>ok</body></html>",
			want:    "<html><body>ok</body></html>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prependDoctype(tt.doctype, tt.html); got != tt.want {
				t.Fatalf("want %q, got %q", tt.want, got)
			}
		})
	}
}
