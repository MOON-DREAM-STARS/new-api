package docker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/browser-agent/internal/runtime"
)

func newTestDriver(server *httptest.Server) *Driver {
	return NewDriver(newClient(server.URL, server.Client()), Options{
		Image:       "newapi-web-workspace-runtime:local",
		Network:     "newapi-workspace-runtimes",
		MemoryBytes: 1073741824,
		NanoCPUs:    1000000000,
		PidsLimit:   256,
	})
}

func TestCreateSendsFrozenContainerSpec(t *testing.T) {
	var (
		requestName string
		requestBody []byte
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestName = r.URL.Query().Get("name")
		requestBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"Id":"container-id","Warnings":[]}`))
	}))
	defer server.Close()

	driver := newTestDriver(server)
	info, err := driver.Create(context.Background(), runtime.CreateSpec{
		WorkspaceID: 5,
		Binds:       []string{"/data/web-workspaces/workspace-5:/workspace"},
		Env:         []string{"WW_WORKSPACE_DIR=/workspace", "WW_PROVIDER=chatgpt"},
	})
	require.NoError(t, err)

	assert.Equal(t, "container-id", info.ID)
	assert.Equal(t, "newapi-ws-runtime-5", requestName)

	var payload createRequest
	require.NoError(t, json.Unmarshal(requestBody, &payload))
	assert.Equal(t, "newapi-web-workspace-runtime:local", payload.Image)
	assert.Equal(t, map[string]string{
		"newapi.web-workspace":              "1",
		"newapi.web-workspace.workspace-id": "5",
	}, payload.Labels)
	assert.Equal(t, []string{"WW_WORKSPACE_DIR=/workspace", "WW_PROVIDER=chatgpt"}, payload.Env)
	assert.Equal(t, []string{"/data/web-workspaces/workspace-5:/workspace"}, payload.HostConfig.Binds)
	assert.Equal(t, "newapi-workspace-runtimes", payload.HostConfig.NetworkMode)
	assert.True(t, payload.HostConfig.ReadonlyRootfs)
	assert.False(t, payload.HostConfig.Privileged)
	assert.False(t, payload.HostConfig.AutoRemove)
	assert.Equal(t, int64(536870912), payload.HostConfig.ShmSize)
	assert.Equal(t, []string{"ALL"}, payload.HostConfig.CapDrop)
	assert.Equal(t, []string{"no-new-privileges:true"}, payload.HostConfig.SecurityOpt)
	assert.Equal(t, "no", payload.HostConfig.RestartPolicy.Name)
	assert.Equal(t, int64(1073741824), payload.HostConfig.Memory)
	assert.Equal(t, int64(1000000000), payload.HostConfig.NanoCpus)
	assert.Equal(t, int64(256), payload.HostConfig.PidsLimit)
	assert.Equal(t, "rw,size=256m,mode=1777", payload.HostConfig.Tmpfs["/tmp"])
	assert.Equal(t, "rw,size=16m,mode=755", payload.HostConfig.Tmpfs["/run"])
	assert.Len(t, payload.HostConfig.Tmpfs, 2)
	assert.Equal(t, "rw,size=256m,mode=1777", payload.Tmpfs["/tmp"])
	assert.Equal(t, "rw,size=16m,mode=755", payload.Tmpfs["/run"])
	assert.Len(t, payload.Tmpfs, 2)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(requestBody, &raw))
	assert.NotContains(t, raw, "User", "the runtime container must use the image default user")
	hostConfig, ok := raw["HostConfig"].(map[string]any)
	require.True(t, ok)
	portBindings, ok := hostConfig["PortBindings"].(map[string]any)
	require.True(t, ok, "PortBindings must be present and empty")
	assert.Empty(t, portBindings)
}

func TestCreateAdoptsRunningContainerOnNameConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"message":"Conflict. The container name is already in use"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/containers/newapi-ws-runtime-5/json":
			_, _ = w.Write([]byte(`{"Id":"existing","State":{"Status":"running","Running":true},"NetworkSettings":{"Networks":{"newapi-workspace-runtimes":{"IPAddress":"10.5.0.9"}}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	driver := newTestDriver(server)
	info, err := driver.Create(context.Background(), runtime.CreateSpec{WorkspaceID: 5})
	require.NoError(t, err)
	assert.True(t, info.Reused)
	assert.Equal(t, "existing", info.ID)
	assert.Equal(t, "10.5.0.9", info.IP)
	assert.True(t, info.Running)
}

func TestCreateRemovesStoppedContainerOnNameConflict(t *testing.T) {
	createCalls := 0
	removeCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/containers/create":
			createCalls++
			if createCalls == 1 {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"message":"conflict"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"fresh"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/containers/newapi-ws-runtime-9/json":
			_, _ = w.Write([]byte(`{"Id":"stale","State":{"Status":"exited","Running":false}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/containers/newapi-ws-runtime-9":
			removeCalls++
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	driver := newTestDriver(server)
	info, err := driver.Create(context.Background(), runtime.CreateSpec{WorkspaceID: 9})
	require.NoError(t, err)
	assert.Equal(t, "fresh", info.ID)
	assert.False(t, info.Reused)
	assert.Equal(t, 2, createCalls)
	assert.Equal(t, 1, removeCalls)
}

func TestInspectMapsNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No such container"}`))
	}))
	defer server.Close()

	driver := newTestDriver(server)
	_, err := driver.Inspect(context.Background(), 5)
	require.ErrorIs(t, err, runtime.ErrNotFound)
}

func TestListParsesWorkspaceLabel(t *testing.T) {
	var filters string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filters = r.URL.Query().Get("filters")
		_, _ = w.Write([]byte(`[{"Id":"a","State":"running","Labels":{"newapi.web-workspace":"1","newapi.web-workspace.workspace-id":"9"}},{"Id":"b","State":"exited","Labels":{"newapi.web-workspace":"1"}}]`))
	}))
	defer server.Close()

	driver := newTestDriver(server)
	infos, err := driver.List(context.Background())
	require.NoError(t, err)
	require.Len(t, infos, 2)
	assert.JSONEq(t, `{"label":["newapi.web-workspace=1"]}`, filters)
	assert.Equal(t, int64(9), infos[0].WorkspaceID)
	assert.True(t, infos[0].Running)
	assert.Equal(t, runtime.ContainerRunning, infos[0].State)
	assert.Equal(t, int64(0), infos[1].WorkspaceID)
	assert.False(t, infos[1].Running)
}

func TestConnectRejectsStoppedContainer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Id":"a","State":{"Status":"exited","Running":false}}`))
	}))
	defer server.Close()

	driver := newTestDriver(server)
	_, err := driver.Connect(context.Background(), 5)
	assert.ErrorIs(t, err, runtime.ErrNotRunning)
}

func TestStopTreatsNotModifiedAsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	driver := newTestDriver(server)
	assert.NoError(t, driver.Stop(context.Background(), 5))
}
