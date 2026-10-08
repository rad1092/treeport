// Package treeport checks complete destination trees without changing the source.
package treeport

const Version = "0.1.0"
const SchemaVersion = "1"

type Entry struct {
	Path string
	Kind string
}

// Paths are slash-delimited relative raw bytes. Kind: file, directory, symlink.
type Limits struct {
	MaxEntries    int
	MaxNodes      int
	MaxPathBytes  int
	MaxTotalBytes int64
	MaxIssues     int
	MaxIndexBytes int64
}

func DefaultLimits() Limits {
	return Limits{MaxEntries: 1000000, MaxNodes: 2000000, MaxPathBytes: 4096, MaxTotalBytes: 128 << 20, MaxIssues: 10000, MaxIndexBytes: 512 << 20}
}

type Options struct {
	Profile         string
	DestinationRoot string
	MaxComponent    int
	MaxPath         int
	Limits          Limits
}
type Original struct {
	Display string `json:"display"`
	Base64  string `json:"base64"`
}
type Issue struct {
	Code   string   `json:"code"`
	Status string   `json:"status"`
	Path   Original `json:"path"`
	Detail string   `json:"detail"`
}
type Member struct {
	Path      Original `json:"path"`
	Kind      string   `json:"kind"`
	Implicit  bool     `json:"implicit"`
	Count     int      `json:"count"`
	Uncertain bool     `json:"uncertain"`
}
type Conflict struct {
	ID      string   `json:"id"`
	Target  string   `json:"target"`
	Status  string   `json:"status"`
	Causes  []string `json:"causes"`
	Members []Member `json:"members"`
}
type Report struct {
	SchemaVersion   string     `json:"schema_version"`
	ToolVersion     string     `json:"tool_version"`
	Profile         string     `json:"profile"`
	ComponentBudget int        `json:"component_budget"`
	PathBudget      int        `json:"path_budget"`
	LengthUnit      string     `json:"length_unit"`
	ProfileVersion  string     `json:"profile_version"`
	UnicodeVersion  string     `json:"unicode_version"`
	DestinationRoot string     `json:"destination_root"`
	Status          string     `json:"status"`
	Complete        bool       `json:"complete"`
	EntryCount      int        `json:"entry_count"`
	NodeCount       int        `json:"node_count"`
	Issues          []Issue    `json:"issues"`
	Conflicts       []Conflict `json:"conflicts"`
	Limitations     []string   `json:"limitations"`
}

// Profiles: posix, windows, macos, export-fold. Model v1; see docs/profiles.md.
// Check sorts its own index and does not mutate entries. Cancellation returns error.
