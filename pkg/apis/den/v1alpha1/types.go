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

// DenFinalizer is on every Den: dennet removes it once the den has stopped,
// so deleting a Den is how to stop its den.
const DenFinalizer = "den.mkm.pub/den"

// UndoFinalizer is on every Space, Surface and Den. A deleted one stays,
// marked by its deletionTimestamp, until dennet removes this finalizer a
// while later (--undo-grace, 1 minute by default). Until then, creating an
// object with the same name brings it back as it was: same uid, status kept,
// and for a Den the same den, which was never stopped. Deleting with
// gracePeriodSeconds=0 skips the wait; UndoGraceAnnotation changes it.
const UndoFinalizer = "den.mkm.pub/undo"

// UndoGraceAnnotation on a Space, Surface or Den sets how long it can be
// brought back once deleted, as a Go duration ("0s", "10m"), in place of
// --undo-grace. "0s" (or "0") means it goes as soon as it is deleted. It can be set
// before the delete or while the object waits. Values that don't parse are
// ignored.
const UndoGraceAnnotation = "den.mkm.pub/undo-grace"

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
	// DenLost is set by dennet when the den's socket closes.
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

	Spec   SpaceSpec   `json:"spec,omitempty"`
	Status SpaceStatus `json:"status,omitempty"`
}

type SpaceSpec struct {
	// Parent is the name of the parent Space in the same namespace.
	// Empty means top level.
	Parent string `json:"parent,omitempty"`
	// Gather names other Spaces in the same namespace that clients also
	// show under this one, in this order, after its own children: for
	// example the worktrees of one piece of work, each from another repo.
	// A gathered Space keeps its home under its Parent, and archiving does
	// not follow Gather. A name that matches no Space shows nothing.
	Gather []string `json:"gather,omitempty"`
	// Order is the position among siblings, lowest first.
	Order int32  `json:"order,omitempty"`
	Title string `json:"title,omitempty"`
	// Subtitle is an optional second line.
	Subtitle string  `json:"subtitle,omitempty"`
	Icon     string  `json:"icon,omitempty"`
	Badges   []Badge `json:"badges,omitempty"`
	// Tags are free-form labels; dennet gives them no meaning.
	Tags []string `json:"tags,omitempty"`
	// State drives the state dot.
	State     SpaceState `json:"state,omitempty"`
	Collapsed bool       `json:"collapsed,omitempty"`
	// WorkingDir is the absolute working directory of dens started for
	// this space's surfaces. Empty leaves it to whoever starts the den.
	WorkingDir string `json:"workingDir,omitempty"`
	// Archived asks dennet to delete the space's Surfaces and their Dens,
	// and to archive every Space under it. The Space itself stays.
	Archived bool `json:"archived,omitempty"`
}

// SpaceStatus is written only by dennet, on spaces/status.
type SpaceStatus struct {
	// ArchivedAt is when dennet finished archiving the space. It is
	// cleared when spec.archived goes back to false.
	ArchivedAt *metav1.Time `json:"archivedAt,omitempty"`
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
	// Order is the position among the space's tabs, or among its Surfaces
	// in the same box, lowest first.
	Order int32  `json:"order,omitempty"`
	Title string `json:"title,omitempty"`
	// Beside is the name of another Surface of the same Space that this one is
	// glued beside, on its right: clients show the two side by side as one tab.
	// A name that matches no such Surface glues nothing.
	Beside string `json:"beside,omitempty"`
	// Placement is where clients show it: a tab when empty, or else the
	// name of a box, a slot in the client's window that shows the selected
	// Space's Surfaces placed there (dense's are under the Space tree).
	// Clients decide where a box goes and how big it is, and give a box to
	// every name a Surface uses, set up or not. One in a box is glued to
	// nothing: it cannot be beside another, and one beside it glues nothing.
	Placement SurfacePlacement `json:"placement,omitempty"`
	// Kind says what the tab shows, and which block below is set.
	Kind     SurfaceKind      `json:"kind"`
	Terminal *TerminalSurface `json:"terminal,omitempty"`
	Web      *WebSurface      `json:"web,omitempty"`
	UI       *UISurface       `json:"ui,omitempty"`
}

// SurfacePlacement is empty for a tab, or a box's name: a DNS label.
type SurfacePlacement string

// SurfaceTab is a tab; any other placement names a box.
const SurfaceTab SurfacePlacement = ""

type SurfaceKind string

const (
	SurfaceTerminal SurfaceKind = "terminal"
	SurfaceWeb      SurfaceKind = "web"
	// SurfaceUI is a tab drawn by an Extension, with A2UI.
	SurfaceUI SurfaceKind = "ui"
)

type TerminalSurface struct {
	// Den is the name of the Den to attach to, in the same namespace.
	Den string `json:"den"`
}

// WebSurface is a tab that shows a web page. Exactly one of URL and HTML is set.
type WebSurface struct {
	// URL is the page to show. A file:// URL names a file on the machine of
	// the client that shows the tab.
	URL string `json:"url,omitempty"`
	// HTML is the page itself, shown instead of a URL. A client shows it
	// again each time it changes.
	HTML string `json:"html,omitempty"`
	// Navigation makes the tab a browser: links open in it, and it has
	// back, forward, reload and an address field. Without it the tab shows
	// only this page, and links open in the default browser.
	Navigation bool `json:"navigation,omitempty"`
}

// UISurface is a Surface whose content an Extension draws: a client connects
// to the Extension's endpoint and asks for the A2UI surface whose surfaceId
// is the Surface's name.
type UISurface struct {
	// Extension is the name of the Extension, in the same namespace.
	Extension string `json:"extension"`
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
// dennet reaches it with ssh, which forwards to the dennet socket there.
type Host struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec HostSpec `json:"spec,omitempty"`
}

type HostSpec struct {
	// SSH is the ssh destination: [user@]host, a Host alias from
	// ~/.ssh/config, or ssh://[user@]host[:port].
	SSH string `json:"ssh"`
	// SocketPath is the absolute path of the dennet socket there. By default a
	// shell there looks where dennet server run puts it.
	SocketPath string `json:"socketPath,omitempty"`
	// DenPath is the absolute path of den there, for clients that run den on
	// the Host, such as dense attaching a terminal. By default they look for
	// den on the PATH of a login shell there.
	DenPath string `json:"denPath,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type HostList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Host `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Extension is a long-running program that shows its own UI in clients
// such as dense, in Surfaces of kind ui: for example ccwt. The program
// creates its Extension and listens on spec.endpoint; clients connect there
// and speak A2UI 1.0 for every Surface of kind ui that names it.
type Extension struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExtensionSpec   `json:"spec,omitempty"`
	Status ExtensionStatus `json:"status,omitempty"`
}

type ExtensionSpec struct {
	// Endpoint is the absolute path of the Unix socket the program listens
	// on, on the machine dennet runs on.
	Endpoint string `json:"endpoint"`
}

// ExtensionStatus is written only by dennet, on extensions/status. dennet
// keeps a connection open to the endpoint: while it is open the Extension
// is Connected. One Disconnected for 10 minutes is deleted, and with it the
// Surfaces it owns.
type ExtensionStatus struct {
	Phase ExtensionPhase `json:"phase,omitempty"`
	// Since is when the phase last changed.
	Since *metav1.Time `json:"since,omitempty"`
}

type ExtensionPhase string

const (
	ExtensionConnected    ExtensionPhase = "Connected"
	ExtensionDisconnected ExtensionPhase = "Disconnected"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ExtensionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Extension `json:"items"`
}
