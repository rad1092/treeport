// Command treeport performs read-only, model-scoped destination tree checks.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/rad1092/treeport"
	"github.com/rad1092/treeport/input"
)

const usage = `Usage: treeport <scan|manifest|zip> --profile PROFILE [flags] INPUT
       treeport version

Commands:
  scan       Enumerate a directory without following symlinks.
  manifest   Read JSONL entries; INPUT may be - for standard input.
  zip        Inspect ZIP entry names without extracting file data.

Profiles: posix, windows, macos, export-fold. A profile is a named model,
not a guarantee about every filesystem. Windows requires --root C:\\target
or an absolute UNC destination. All flags must precede INPUT.

Exit codes: 0 known-compatible; 1 incompatible; 2 usage/input/runtime error;
3 unknown. --json sends one report or error object to stdout.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

type errorReport struct {
	SchemaVersion string `json:"schema_version"`
	ToolVersion   string `json:"tool_version"`
	Status        string `json:"status"`
	Error         string `json:"error"`
}

func fail(out, stderr io.Writer, asJSON bool, err error) int {
	if asJSON {
		if e := json.NewEncoder(out).Encode(errorReport{treeport.SchemaVersion, treeport.Version, "error", err.Error()}); e != nil {
			fmt.Fprintf(stderr, "treeport: writing error report: %v\n", e)
		}
	} else {
		fmt.Fprintf(stderr, "treeport: %v\n", err)
	}
	return 2
}

// Honor --json for parse errors too, including an error before that flag.
func wantsJSON(args []string) bool {
	var enabled bool
	for _, arg := range args {
		if arg == "--" {
			break
		}
		switch arg {
		case "--json", "-json", "--json=true", "-json=true":
			enabled = true
		case "--json=false", "-json=false":
			enabled = false
		}
	}
	return enabled
}

func run(parent context.Context, args []string, stdin io.Reader, out, stderr io.Writer) int {
	asJSON := wantsJSON(args)
	if len(args) == 0 {
		return fail(out, stderr, asJSON, errors.New("missing command; use treeport help"))
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, usage)
		return 0
	}
	if args[0] == "version" || args[0] == "--version" {
		if len(args) != 1 {
			return fail(out, stderr, asJSON, errors.New("version does not accept arguments"))
		}
		fmt.Fprintf(out, "treeport %s\n", treeport.Version)
		return 0
	}
	command := args[0]
	if command != "scan" && command != "manifest" && command != "zip" {
		return fail(out, stderr, asJSON, fmt.Errorf("unknown command %q; use treeport help", command))
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	limits := treeport.DefaultLimits()
	var opts treeport.Options
	var timeout time.Duration
	flags.StringVar(&opts.Profile, "profile", "", "required destination model: posix, windows, macos, export-fold")
	flags.StringVar(&opts.DestinationRoot, "root", "", "destination root, required absolute Windows path for windows")
	flags.IntVar(&opts.MaxComponent, "max-component", 0, "override target component budget; 0 uses profile default")
	flags.IntVar(&opts.MaxPath, "max-path", 0, "override target complete path budget including root; 0 uses profile default")
	flags.IntVar(&limits.MaxEntries, "max-entries", limits.MaxEntries, "maximum input entries")
	flags.IntVar(&limits.MaxNodes, "max-nodes", limits.MaxNodes, "maximum indexed nodes, including implicit parents")
	flags.IntVar(&limits.MaxPathBytes, "max-path-bytes", limits.MaxPathBytes, "maximum raw bytes per input path")
	flags.Int64Var(&limits.MaxTotalBytes, "max-total-bytes", limits.MaxTotalBytes, "maximum total input path bytes (and bounded serialized input)")
	flags.Int64Var(&limits.MaxIndexBytes, "max-index-bytes", limits.MaxIndexBytes, "maximum estimated retained index bytes; not a process RSS cap")
	flags.IntVar(&limits.MaxIssues, "max-issues", limits.MaxIssues, "maximum reported diagnostics")
	flags.DurationVar(&timeout, "timeout", 30*time.Second, "deadline for input and analysis, e.g. 2m; must be positive")
	flags.BoolVar(&asJSON, "json", asJSON, "emit machine-readable JSON to stdout")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(out, usage)
			flags.SetOutput(out)
			flags.PrintDefaults()
			return 0
		}
		return fail(out, stderr, asJSON, err)
	}
	if flags.NArg() != 1 {
		return fail(out, stderr, asJSON, errors.New("exactly one INPUT is required; put all flags before INPUT"))
	}
	if opts.Profile == "" {
		return fail(out, stderr, asJSON, errors.New("--profile is required"))
	}
	if timeout <= 0 || limits.MaxEntries <= 0 || limits.MaxNodes <= 0 || limits.MaxPathBytes <= 0 || limits.MaxTotalBytes <= 0 || limits.MaxIndexBytes <= 0 || limits.MaxIssues <= 0 || opts.MaxComponent < 0 || opts.MaxPath < 0 {
		return fail(out, stderr, asJSON, errors.New("input/report limits and timeout must be positive; target budget overrides must be nonnegative"))
	}
	opts.Limits = limits
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	// Validate profile and root before enumerating a potentially large input.
	if _, err := treeport.Check(ctx, nil, opts); err != nil {
		return fail(out, stderr, asJSON, err)
	}
	var entries []treeport.Entry
	var err error
	source := flags.Arg(0)
	switch command {
	case "scan":
		entries, err = input.Tree(ctx, source, limits)
	case "zip":
		entries, err = input.ZIP(ctx, source, limits)
	case "manifest":
		reader := stdin
		if source != "-" {
			var info os.FileInfo
			info, err = os.Stat(source)
			if err != nil {
				break
			}
			if !info.Mode().IsRegular() {
				err = errors.New("manifest INPUT must be a regular file; use - for a pipe on standard input")
				break
			}
			var file *os.File
			file, err = os.Open(source)
			if err != nil {
				break
			}
			defer file.Close()
			reader = file
		}
		entries, err = readManifest(ctx, reader, limits)
	}
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return fail(out, stderr, asJSON, err)
	}
	report, err := treeport.Check(ctx, entries, opts)
	if err != nil {
		return fail(out, stderr, asJSON, err)
	}
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		err = encoder.Encode(report)
	} else {
		err = writeHuman(out, report)
	}
	if err != nil {
		fmt.Fprintf(stderr, "treeport: writing report: %v\n", err)
		return 2
	}
	switch report.Status {
	case "known-compatible":
		return 0
	case "incompatible":
		return 1
	default:
		return 3
	}
}

func readManifest(ctx context.Context, reader io.Reader, limits treeport.Limits) ([]treeport.Entry, error) {
	type result struct {
		entries []treeport.Entry
		err     error
	}
	ready := make(chan result, 1)
	go func() {
		entries, err := input.Manifest(ctx, reader, limits)
		ready <- result{entries, err}
	}()
	select {
	case value := <-ready:
		return value.entries, value.err
	case <-ctx.Done():
		// Inherited blocking stdin descriptors cannot be interrupted with Close
		// on every OS. Return at the CLI boundary regardless; main exits and
		// the OS reclaims a pending read. There is only one reader per command.
		if closer, ok := reader.(io.Closer); ok {
			go closer.Close()
		}
		return nil, ctx.Err()
	}
}

func writeHuman(out io.Writer, report treeport.Report) error {
	// Compose once so a failed writer is visible to the caller. %q escapes
	// control characters, keeping untrusted filenames out of terminal controls.
	var b strings.Builder
	fmt.Fprintf(&b, "treeport %s: %s\n", report.ToolVersion, report.Status)
	fmt.Fprintf(&b, "profile=%s@%s unicode=%s entries=%d nodes=%d complete=%t\n", report.Profile, report.ProfileVersion, report.UnicodeVersion, report.EntryCount, report.NodeCount, report.Complete)
	if report.DestinationRoot != "" {
		fmt.Fprintf(&b, "destination root: %q\n", report.DestinationRoot)
	}
	for _, issue := range report.Issues {
		fmt.Fprintf(&b, "[%s] %s %q: %s\n", issue.Status, issue.Code, issue.Path.Display, issue.Detail)
	}
	for _, conflict := range report.Conflicts {
		fmt.Fprintf(&b, "[%s] conflict %s -> %q (%s)\n", conflict.Status, conflict.ID, conflict.Target, strings.Join(conflict.Causes, ", "))
		for _, member := range conflict.Members {
			fmt.Fprintf(&b, "  %q kind=%s implicit=%t count=%d\n", member.Path.Display, member.Kind, member.Implicit, member.Count)
		}
	}
	for _, limitation := range report.Limitations {
		fmt.Fprintf(&b, "limitation: %s\n", limitation)
	}
	_, err := io.WriteString(out, b.String())
	return err
}
