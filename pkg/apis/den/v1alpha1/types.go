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

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Space is a node in dense's sidebar tree. Outside tools (such as ccwt)
// decide what a space means; dense only draws it.
type Space struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SpaceSpec `json:"spec,omitempty"`
}

type SpaceSpec struct {
	// Parent is the name of the parent Space in the same namespace.
	// Empty means top level.
	Parent string `json:"parent,omitempty"`
	// Order is the position among siblings, lowest first.
	Order int32  `json:"order,omitempty"`
	Title string `json:"title,omitempty"`
	// Subtitle is an optional second line.
	Subtitle string  `json:"subtitle,omitempty"`
	Icon     string  `json:"icon,omitempty"`
	Badges   []Badge `json:"badges,omitempty"`
	// State drives the state dot.
	State     SpaceState `json:"state,omitempty"`
	Collapsed bool       `json:"collapsed,omitempty"`
}

type Badge struct {
	Text string `json:"text"`
	// Tone is a styling hint, e.g. info or muted.
	Tone string `json:"tone,omitempty"`
}

type SpaceState string

const (
	SpaceIdle    SpaceState = "idle"
	SpaceBusy    SpaceState = "busy"
	SpaceWaiting SpaceState = "waiting"
	SpaceError   SpaceState = "error"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SpaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Space `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Surface is one tab inside a Space.
type Surface struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SurfaceSpec `json:"spec,omitempty"`
}

type SurfaceSpec struct {
	// Space is the name of the Space this tab belongs to, in the same namespace.
	Space string `json:"space"`
	// Order is the position among the space's tabs, lowest first.
	Order int32  `json:"order,omitempty"`
	Title string `json:"title,omitempty"`
	// Kind says what the tab shows, and which block below is set.
	Kind     SurfaceKind      `json:"kind"`
	Terminal *TerminalSurface `json:"terminal,omitempty"`
}

type SurfaceKind string

const SurfaceTerminal SurfaceKind = "terminal"

type TerminalSurface struct {
	// Den is the name of the Den to attach to, in the same namespace.
	Den string `json:"den"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type SurfaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Surface `json:"items"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Host is another machine running dennet. Its objects show up here in the
// namespace with the Host's name; there they live in its default namespace.
// dennet reaches it with ssh, running dennet server dial-stdio there.
type Host struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec HostSpec `json:"spec,omitempty"`
}

type HostSpec struct {
	// SSH is the ssh destination: [user@]host, a Host alias from
	// ~/.ssh/config, or ssh://[user@]host[:port].
	SSH string `json:"ssh"`
	// Dennet is the command that runs dennet there, read by the remote
	// shell, e.g. "~/go/bin/dennet --socket=/run/x.sock". Defaults to dennet.
	Dennet string `json:"dennet,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HostList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Host `json:"items"`
}
