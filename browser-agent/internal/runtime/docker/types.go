package docker

// createRequest is the subset of the Docker Engine create-container payload the
// agent relies on. Every security relevant option is fixed; only the image,
// environment, bind mount and resource limits vary.
type createRequest struct {
	Image      string            `json:"Image"`
	Env        []string          `json:"Env,omitempty"`
	Labels     map[string]string `json:"Labels,omitempty"`
	Tmpfs      map[string]string `json:"Tmpfs,omitempty"`
	HostConfig hostConfig        `json:"HostConfig"`
}

type hostConfig struct {
	Binds          []string          `json:"Binds,omitempty"`
	NetworkMode    string            `json:"NetworkMode"`
	PortBindings   map[string]any    `json:"PortBindings"`
	ReadonlyRootfs bool              `json:"ReadonlyRootfs"`
	ShmSize        int64             `json:"ShmSize"`
	CapDrop        []string          `json:"CapDrop"`
	SecurityOpt    []string          `json:"SecurityOpt"`
	Privileged     bool              `json:"Privileged"`
	AutoRemove     bool              `json:"AutoRemove"`
	RestartPolicy  restartPolicy     `json:"RestartPolicy"`
	Memory         int64             `json:"Memory"`
	NanoCpus       int64             `json:"NanoCpus"`
	PidsLimit      int64             `json:"PidsLimit"`
	Tmpfs          map[string]string `json:"Tmpfs,omitempty"`
}

type restartPolicy struct {
	Name string `json:"Name"`
}

// inspectResponse is the subset of GET /containers/{id}/json the agent reads.
type inspectResponse struct {
	ID     string `json:"Id"`
	Config struct {
		Env []string `json:"Env"`
	} `json:"Config"`
	State struct {
		Status    string `json:"Status"`
		Running   bool   `json:"Running"`
		StartedAt string `json:"StartedAt"`
	} `json:"State"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

// NetworkInfo is the subset of GET /networks/{name} the agent reads.
type NetworkInfo struct {
	ID       string `json:"Id"`
	Name     string `json:"Name"`
	Driver   string `json:"Driver"`
	Internal bool   `json:"Internal"`
}

// networkCreateRequest is the fixed payload used to create the private runtime
// network. Runtime containers must never be attached to a non-internal bridge.
type networkCreateRequest struct {
	Name     string `json:"Name"`
	Driver   string `json:"Driver"`
	Internal bool   `json:"Internal"`
}
