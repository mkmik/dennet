package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Den is one background terminal session, run by a den process.
// spec is written by whoever creates the Den; status is written only by den.
type Den struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DenSpec   `json:"spec,omitempty"`
	Status DenStatus `json:"status,omitempty"`
}

// DenSpec follows Spec in den's src/spec.zig. Report, HostName and
// RestartPolicy are dennet's own; den ignores them.
type DenSpec struct {
	// Command is the program and its arguments. Empty means the user's
	// shell as a login shell. Immutable.
	Command    []string `json:"command,omitempty"`
	WorkingDir string   `json:"workingDir,omitempty"`
	// Env is added to the environment; it wins over inherited values.
	Env []EnvVar `json:"env,omitempty"`
	// InheritEnv names variables copied from the environment den was
	// started with: exact names, or prefixes ending in "*". ["*"] copies all.
	InheritEnv []string `json:"inheritEnv,omitempty"`
	// Size of the PTY while no client is attached.
	Size *Size `json:"size,omitempty"`
	// Paused stops the program (SIGSTOP) while true.
	Paused  bool        `json:"paused,omitempty"`
	History HistorySpec `json:"history,omitempty"`
	Report  ReportSpec  `json:"report,omitempty"`
	// HostName is the Host that runs this den. Empty means the host dennet runs on.
	HostName string `json:"hostName,omitempty"`
	// RestartPolicy is Never (default) or OnFailure. Handled by dennet, not den.
	RestartPolicy RestartPolicy `json:"restartPolicy,omitempty"`
}

type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

type Size struct {
	Cols int32 `json:"cols"`
	Rows int32 `json:"rows"`
}

type HistorySpec struct {
	// MemoryLines is the scrollback kept in memory. Defaults to 5000.
	MemoryLines int32 `json:"memoryLines,omitempty"`
	// KeepAfterExit is how long an exited den keeps its socket and history.
	KeepAfterExit *metav1.Duration `json:"keepAfterExit,omitempty"`
}

type ReportSpec struct {
	// Fields lists the status fields den publishes, e.g. title, cwd, screen, clients.
	Fields   []string         `json:"fields,omitempty"`
	Interval *metav1.Duration `json:"interval,omitempty"`
}

type RestartPolicy string

const (
	RestartPolicyNever     RestartPolicy = "Never"
	RestartPolicyOnFailure RestartPolicy = "OnFailure"
)

type DenPhase string

const (
	DenPending DenPhase = "Pending"
	DenRunning DenPhase = "Running"
	DenExited  DenPhase = "Exited"
	DenFailed  DenPhase = "Failed"
	// DenLost is set by dennet when the den's Lease expires.
	DenLost DenPhase = "Lost"
)

type DenStatus struct {
	Phase  DenPhase `json:"phase,omitempty"`
	PID    int32    `json:"pid,omitempty"`
	Host   string   `json:"host,omitempty"`
	Socket string   `json:"socket,omitempty"`
	// Log is den's log file, next to the socket.
	Log            string       `json:"log,omitempty"`
	Clients        int32        `json:"clients,omitempty"`
	Screen         string       `json:"screen,omitempty"`
	Title          string       `json:"title,omitempty"`
	CWD            string       `json:"cwd,omitempty"`
	LastOutputTime *metav1.Time `json:"lastOutputTime,omitempty"`
	ExitCode       *int32       `json:"exitCode,omitempty"`
	// ObservedGeneration is the spec generation den has applied.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type DenList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Den `json:"items"`
}
