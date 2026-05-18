package pipeline

import "testing"

func TestParseSourceFilter(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantType  string
		wantQuery string
	}{
		{"no prefix", "what is rag?", "", "what is rag?"},
		{"pdf prefix", "source:pdf what does the lease say?", "pdf", "what does the lease say?"},
		{"mbox prefix", "source:mbox when did Sarah email?", "mbox", "when did Sarah email?"},
		{"uppercase type normalized", "source:PDF hello", "pdf", "hello"},
		{"unknown type passes through", "source:web search me", "", "source:web search me"},
		{"prefix only, no query", "source:pdf", "", "source:pdf"},
		{"leading whitespace stripped", "   source:pdf hello", "pdf", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotT, gotQ := parseSourceFilter(tt.in)
			if gotT != tt.wantType {
				t.Errorf("type: got %q, want %q", gotT, tt.wantType)
			}
			if gotQ != tt.wantQuery {
				t.Errorf("query: got %q, want %q", gotQ, tt.wantQuery)
			}
		})
	}
}
