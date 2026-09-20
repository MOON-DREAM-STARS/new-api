package webworkspace

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentClientMapsRuntimeCapacityReached(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"success":false,"error":"runtime_capacity_reached"}`))
	}))
	defer server.Close()

	client := &AgentClient{
		baseURL: server.URL,
		token:   "test-token",
		client:  server.Client(),
	}
	err := client.do(context.Background(), http.MethodPost, "/capacity", nil, nil)
	require.ErrorIs(t, err, ErrCapacityReached)
}
