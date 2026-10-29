package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelsHandler(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want string
	}{
		{
			name: "two models in config order",
			ids:  []string{"gpt-6.1-sol", "claude-opus-5-5"},
			want: `{"object":"list","data":[` +
				`{"id":"gpt-6.1-sol","object":"model","created":0,"owned_by":"llm-gateway"},` +
				`{"id":"claude-opus-5-5","object":"model","created":0,"owned_by":"llm-gateway"}]}` + "\n",
		},
		{
			name: "no models gives an empty array, not null",
			ids:  nil,
			want: `{"object":"list","data":[]}` + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &gateway{models: newModelList(tt.ids)}
			rec := httptest.NewRecorder()

			g.modelsHandler(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want %q", got, "application/json")
			}
			// Encode adds a newline after the JSON, so want ends with "\n".
			if got := rec.Body.String(); got != tt.want {
				t.Errorf("body:\n got %s\nwant %s", got, tt.want)
			}
		})
	}
}
