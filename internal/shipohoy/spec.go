package shipohoy

import (
	"io"
	"time"
)

// YardPlan configures default behavior for all containers in a yard.
type YardPlan struct {
	NamePrefix   string
	Env          map[string]string
	Labels       map[string]string
	ResourceCaps ResourceCaps
}

// ResourceCaps sets optional resource limits (0 means default).
type ResourceCaps struct {
	MemoryBytes int64
	NanoCPUs    int64
}

// Mount describes a host mount to place inside a container.
type Mount struct {
	Source      string
	Target      string
	ReadOnly    bool
	Propagation string
}

// TmpfsMount describes a tmpfs mount inside the container.
type TmpfsMount struct {
	Target  string
	Options []string
}

// ContainerSpec describes a container.
type ContainerSpec struct {
	Name           string
	Image          string
	Snapshotter    string
	Env            map[string]string
	Labels         map[string]string
	Command        []string
	WorkingDir     string
	Mounts         []Mount
	Tmpfs          []TmpfsMount
	ReadOnlyRootfs bool
	AutoRemove     bool
	ResourceCaps   *ResourceCaps
	HostNetwork    bool
	LogBufferBytes int
}

// BuildSpec describes a container image build.
type BuildSpec struct {
	ContextDir        string
	ContainerfilePath string
	ContainerfileData []byte
	Tags              []string
	BuildArgs         map[string]string
	Timeout           time.Duration
	OutputPath        string
}

// BuildResult captures build output metadata.
type BuildResult struct {
	ImageNames []string
}

// BuildEventKind categorizes build progress updates.
type BuildEventKind string

const (
	// BuildEventVertexStarted marks a build vertex start event.
	BuildEventVertexStarted BuildEventKind = "vertex_started"
	// BuildEventVertexCompleted marks a build vertex completion event.
	BuildEventVertexCompleted BuildEventKind = "vertex_completed"
	// BuildEventLog indicates a build log event.
	BuildEventLog BuildEventKind = "log"
	// BuildEventWarning indicates a build warning event.
	BuildEventWarning BuildEventKind = "warning"
)

// BuildEvent reports a build progress update.
type BuildEvent struct {
	Kind      BuildEventKind
	VertexID  string
	Name      string
	Message   string
	Timestamp time.Time
	Error     string
}

// ExecSpec describes a command execution inside a running container.
type ExecSpec struct {
	Command    []string
	Env        map[string]string
	WorkingDir string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Timeout    time.Duration
}

// ExecResult captures exec completion metadata.
type ExecResult struct {
	ExitCode int
	Started  time.Time
	Finished time.Time
}

// LogStream selects which logs to search.
type LogStream int

const (
	// LogStdout selects stdout logs.
	LogStdout LogStream = iota
	// LogStderr selects stderr logs.
	LogStderr
	// LogBoth selects both stdout and stderr logs.
	LogBoth
)

// WaitLogSpec waits for a log substring.
type WaitLogSpec struct {
	Text     string
	Stream   LogStream
	Timeout  time.Duration
	Interval time.Duration
}

// WaitPortSpec waits for a TCP port to accept connections.
type WaitPortSpec struct {
	Address       string
	Port          int
	Timeout       time.Duration
	Interval      time.Duration
	NetNSFallback bool
}

// JanitorSpec prunes managed containers.
type JanitorSpec struct {
	LabelSelector map[string]string
	MinAge        time.Duration
}

// Container is implemented by runtime-specific adapters.
type Container interface {
	Name() string
	Spec() ContainerSpec
}

// mergeSpec overlays yard defaults onto container spec.
func mergeSpec(spec ContainerSpec, plan YardPlan) ContainerSpec {
	out := spec
	if out.Env == nil {
		out.Env = map[string]string{}
	}
	if out.Labels == nil {
		out.Labels = map[string]string{}
	}
	for k, v := range plan.Env {
		if _, ok := out.Env[k]; !ok {
			out.Env[k] = v
		}
	}
	for k, v := range plan.Labels {
		if _, ok := out.Labels[k]; !ok {
			out.Labels[k] = v
		}
	}
	if plan.NamePrefix != "" {
		out.Name = plan.NamePrefix + out.Name
	}
	if out.ResourceCaps == nil {
		out.ResourceCaps = &plan.ResourceCaps
	} else {
		if out.ResourceCaps.MemoryBytes == 0 {
			out.ResourceCaps.MemoryBytes = plan.ResourceCaps.MemoryBytes
		}
		if out.ResourceCaps.NanoCPUs == 0 {
			out.ResourceCaps.NanoCPUs = plan.ResourceCaps.NanoCPUs
		}
	}
	return out
}
