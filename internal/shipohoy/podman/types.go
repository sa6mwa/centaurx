package podman

type createResponse struct {
	ID       string   `json:"Id"`
	Warnings []string `json:"Warnings"`
}

type inspectContainer struct {
	ID     string `json:"Id"`
	Name   string `json:"Name"`
	Config struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	HostConfig struct {
		AutoRemove bool `json:"AutoRemove"`
	} `json:"HostConfig"`
	State struct {
		Running  bool   `json:"Running"`
		Status   string `json:"Status"`
		ExitCode int    `json:"ExitCode"`
	} `json:"State"`
}

type execCreateResponse struct {
	ID string `json:"Id"`
}

type execInspect struct {
	Running  bool `json:"Running"`
	ExitCode int  `json:"ExitCode"`
}

type containerListItem struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Created int64             `json:"Created"`
	Labels  map[string]string `json:"Labels"`
}

type buildResponse struct {
	Stream      string `json:"stream"`
	Error       string `json:"error"`
	ErrorDetail struct {
		Message string `json:"message"`
	} `json:"errorDetail"`
}
