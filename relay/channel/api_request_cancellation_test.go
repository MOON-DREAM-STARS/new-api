package channel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cancellationAdaptor struct {
	Adaptor
	url string
}

func (a cancellationAdaptor) GetRequestURL(*relaycommon.RelayInfo) (string, error) {
	return a.url, nil
}

func (cancellationAdaptor) SetupRequestHeader(*gin.Context, *http.Header, *relaycommon.RelayInfo) error {
	return nil
}

func TestDoApiRequestCancelsStreamBeforeUpstreamHeaders(t *testing.T) {
	service.InitHttpClient()
	entered := make(chan struct{})
	upstreamDone := make(chan error, 1)
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-r.Context().Done():
			upstreamDone <- r.Context().Err()
		case <-release:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer upstream.Close()
	defer close(release)

	requestContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", http.NoBody).WithContext(requestContext)
	info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{}}
	relayDone := make(chan error, 1)
	go func() {
		response, err := DoApiRequest(cancellationAdaptor{url: upstream.URL}, c, info, strings.NewReader(`{"stream":true}`))
		if response != nil {
			_ = response.Body.Close()
		}
		relayDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not receive request")
	}
	cancel()
	select {
	case err := <-upstreamDone:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("upstream kept waiting after the client canceled")
	}
	select {
	case err := <-relayDone:
		require.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not return after the client canceled")
	}
}
