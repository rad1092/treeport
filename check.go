package treeport

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const (
	causeCase uint16 = 1 << iota
	causeNorm
	causeReplace
	causeTrim
	causeTruncate
	causeParent
	causeDuplicate
	causeType
	causeKind
)

var causeNames = []string{"case_fold", "unicode_normalization", "character_replacement", "trailing_dot_space", "truncation", "parent_alias", "duplicate_entry", "file_directory", "entry_type"}

type nodeKey struct {
	path string
	kind string
}
type indexedNode struct {
	source, raw, target, kind string
	explicit                  int
	causes                    uint16
	uncertain                 bool
	spellings                 map[string]int
}

func (n *indexedNode) addExplicit(raw string) {
	if n.explicit > 0 && raw != n.raw && n.spellings == nil {
		n.spellings = map[string]int{n.raw: n.explicit}
	}
	if n.spellings != nil {
		n.spellings[raw]++
	}
	n.explicit++
	if n.raw == n.source || raw < n.raw {
		n.raw = raw
	}
}

type profile struct {
	name            string
	version         string
	component, path int
	utf16           bool
}

// NormalizeLimits replaces zero values with defaults and rejects negative limits.
func NormalizeLimits(l Limits) (Limits, error) {
	d := DefaultLimits()
	if l.MaxEntries == 0 {
		l.MaxEntries = d.MaxEntries
	}
	if l.MaxNodes == 0 {
		l.MaxNodes = d.MaxNodes
	}
	if l.MaxPathBytes == 0 {
		l.MaxPathBytes = d.MaxPathBytes
	}
	if l.MaxTotalBytes == 0 {
		l.MaxTotalBytes = d.MaxTotalBytes
	}
	if l.MaxIndexBytes == 0 {
		l.MaxIndexBytes = d.MaxIndexBytes
	}
	if l.MaxIssues == 0 {
		l.MaxIssues = d.MaxIssues
	}
	if l.MaxEntries < 1 || l.MaxNodes < 1 || l.MaxPathBytes < 1 || l.MaxTotalBytes < 1 || l.MaxIssues < 1 || l.MaxIndexBytes < 1 {
		return l, errors.New("limits must be positive")
	}
	return l, nil
}

func OriginalPath(s string) Original {
	return Original{strings.ToValidUTF8(s, "\uFFFD"), base64.StdEncoding.EncodeToString([]byte(s))}
}
func ascii(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 128 {
			return false
		}
	}
	return true
}
func units(s string, utf16 bool) int {
	if !utf16 {
		return len(s)
	}
	n := 0
	for _, r := range s {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}
func trimUnits(s string, n int) string {
	count := 0
	for i, r := range s {
		v := 1
		if r > 0xffff {
			v = 2
		}
		if count+v > n {
			return s[:i]
		}
		count += v
	}
	return s
}

// Reserved console aliases also open device handles:
// https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilea#consoles
func reserved(s string) bool {
	s = strings.ToUpper(strings.TrimRight(strings.SplitN(s, ".", 2)[0], " "))
	switch s {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	if strings.HasPrefix(s, "COM") || strings.HasPrefix(s, "LPT") {
		return strings.Contains("123456789¹²³", s[3:]) && len([]rune(s[3:])) == 1
	}
	return false
}
func illegal(r rune) bool { return r < 32 || strings.ContainsRune("<>:\"\\|?*", r) }
func absoluteWindows(s string) bool {
	if strings.Contains(s, "/") {
		return false
	}
	var rest string
	if len(s) >= 3 && ((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) && s[1] == ':' && s[2] == '\\' {
		rest = s[3:]
	} else if strings.HasPrefix(s, `\\`) {
		parts := strings.Split(s[2:], `\`)
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return false
		}
		rest = s[2:]
	} else {
		return false
	}
	for _, c := range strings.Split(strings.TrimSuffix(rest, `\`), `\`) {
		if c == "" {
			if rest == "" {
				continue
			}
			return false
		}
		if c == "." || c == ".." || reserved(c) || strings.TrimRight(c, " .") != c || strings.IndexFunc(c, illegal) >= 0 {
			return false
		}
	}
	return true
}

func getProfile(o Options) (profile, error) {
	p := profile{name: o.Profile, version: "1"}
	switch o.Profile {
	case "posix":
		p.version = "2"
		p.component = 255
		p.path = 4095
	case "windows":
		p.version = "2"
		p.component = 255
		p.path = 259
		p.utf16 = true
	case "macos":
		p.version = "2"
		p.component = 255
		p.path = 1023
	case "export-fold":
		p.version = "2"
		p.component = 255
		p.path = 259
		p.utf16 = true
	default:
		return p, fmt.Errorf("unknown profile %q (choose posix, windows, macos, export-fold)", o.Profile)
	}
	if o.MaxComponent < 0 || o.MaxPath < 0 {
		return p, errors.New("path budgets must be positive")
	}
	if o.MaxComponent > 0 {
		p.component = o.MaxComponent
	}
	if o.MaxPath > 0 {
		p.path = o.MaxPath
	}
	if o.Profile == "windows" && (!absoluteWindows(o.DestinationRoot) || strings.HasPrefix(o.DestinationRoot, `\\?\`) || strings.HasPrefix(o.DestinationRoot, `\\.\`)) {
		return p, errors.New("windows profile requires an absolute drive or UNC --root; device/extended namespaces are unsupported")
	}
	if p.name == "windows" {
		rootParts := strings.Split(strings.Trim(o.DestinationRoot, `\`), `\`)
		for i, c := range rootParts {
			if i == 0 && strings.HasSuffix(c, ":") {
				continue
			}
			if units(c, true) > p.component {
				return p, errors.New("destination root component exceeds component budget")
			}
		}
	}
	if !utf8.ValidString(o.DestinationRoot) || strings.ContainsRune(o.DestinationRoot, 0) {
		return p, errors.New("destination root must be valid UTF-8 without NUL")
	}
	return p, nil
}

// Check evaluates names against an explicit, versioned model. A compatible result
// applies only to that model, not all mounts, OS APIs, or concurrent filesystem changes.
// Check never changes entries or reads their file contents. Partial scans return errors.
func Check(ctx context.Context, entries []Entry, opts Options) (report Report, retErr error) {
	defer func() {
		if retErr != nil {
			report.Complete = false
			report.Status = "unknown"
		}
	}()
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	p, err := getProfile(opts)
	if err != nil {
		return Report{}, err
	}
	lim, err := NormalizeLimits(opts.Limits)
	if err != nil {
		return Report{}, err
	}
	report = Report{SchemaVersion: SchemaVersion, ToolVersion: Version, Profile: p.name, ProfileVersion: p.version, UnicodeVersion: norm.Version, DestinationRoot: opts.DestinationRoot, Status: "known-compatible", Complete: true, EntryCount: len(entries), Issues: []Issue{}, Conflicts: []Conflict{}, Limitations: []string{"Model-scoped filename preflight; not a universal portability or security guarantee.", "No destination mount inspection, permissions/content validation, hardlink identity or path race guarantee.", "Symlinks are not followed; existing destination contents are not scanned."}}
	report.ComponentBudget = p.component
	report.PathBudget = p.path
	report.LengthUnit = "bytes"
	if p.utf16 {
		report.LengthUnit = "utf16"
	}
	if p.name == "windows" || p.name == "macos" {
		report.Limitations = append(report.Limitations, "Non-ASCII comparison is a Unicode folding candidate, not the filesystem's exact versioned comparison table; affected names are unknown.")
	}
	// Destination syntax is profile-specific. Backslash is a literal byte
	// in POSIX/macOS roots, even though input manifests use a stricter grammar.
	rootSeparators := "/"
	if p.name == "windows" || p.name == "export-fold" {
		rootSeparators = "/\\"
	}
	budgetRoot := strings.TrimRight(opts.DestinationRoot, rootSeparators)
	if len(entries) > lim.MaxEntries {
		return report, fmt.Errorf("entry budget exceeded (%d)", lim.MaxEntries)
	}
	total := int64(0)
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if len(e.Path) > lim.MaxPathBytes || int64(len(e.Path)) > lim.MaxTotalBytes-total {
			return report, errors.New("input path byte budget exceeded")
		}
		total += int64(len(e.Path))
		if e.Kind != "" && e.Kind != "file" && e.Kind != "directory" && e.Kind != "symlink" {
			return report, fmt.Errorf("unsupported entry kind %q", e.Kind)
		}
	}
	capacity := min(len(entries), lim.MaxNodes)
	if int64(capacity) > lim.MaxIndexBytes/128 {
		capacity = int(lim.MaxIndexBytes / 128)
	}
	nodes := make(map[nodeKey]*indexedNode, capacity)
	indexBytes := int64(0)
	truncated := false
	addIssue := func(code, status, path, detail string) {
		if status == "incompatible" {
			report.Status = "incompatible"
		} else if report.Status == "known-compatible" {
			report.Status = "unknown"
		}
		if len(report.Issues) < lim.MaxIssues {
			report.Issues = append(report.Issues, Issue{code, status, OriginalPath(path), detail})
		} else {
			truncated = true
		}
	}
	// Copy only the entry descriptors. Sorting gives stable bounded diagnostics even
	// when the input enumeration order changes or report limits are reached.
	ordered := append([]Entry(nil), entries...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Path != ordered[j].Path {
			return ordered[i].Path < ordered[j].Path
		}
		return ordered[i].Kind < ordered[j].Kind
	})
	fold := cases.Fold()
	for _, e := range ordered {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if e.Kind == "" {
			e.Kind = "file"
		}
		if e.Kind != "file" && e.Kind != "directory" && e.Kind != "symlink" {
			return report, fmt.Errorf("unsupported entry kind %q", e.Kind)
		}
		path := e.Path
		if e.Kind == "directory" {
			path = strings.TrimSuffix(path, "/")
		}
		unsafe := path == "" || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\\x00") || (len(path) >= 2 && path[1] == ':')
		components := strings.Split(path, "/")
		for _, c := range components {
			if c == "" || c == "." || c == ".." {
				unsafe = true
			}
		}
		if unsafe {
			addIssue("unsafe_path", "incompatible", e.Path, "expected a relative slash-delimited path without dot segments, NUL, drive prefixes or backslashes")
			continue
		}
		if !utf8.ValidString(path) && p.name != "posix" {
			addIssue("invalid_utf8", "unknown", e.Path, "raw bytes cannot be compared by this Unicode destination model; valid ancestor prefixes are still checked")
		}
		if e.Kind == "symlink" {
			addIssue("symlink", "unknown", e.Path, "link target and destination link behavior are not evaluated")
		}
		sourcePrefix, targetPrefix := "", ""
		budgetPath := ""
		var inherited uint16
		uncertain := false
		for i, c := range components {
			if !utf8.ValidString(c) && p.name != "posix" {
				break
			}
			original := c
			var causes uint16
			if i > 0 {
				sourcePrefix += "/"
				targetPrefix += "/"
			}
			sourcePrefix += original
			if (p.name == "windows" || p.name == "macos") && !ascii(c) {
				uncertain = true
				addIssue("unknown_semantics", "unknown", e.Path, "non-ASCII comparison needs destination filesystem tables; candidate groups use Unicode "+norm.Version)
			}
			if p.name == "windows" || p.name == "export-fold" {
				if p.name == "windows" {
					if strings.IndexFunc(c, illegal) >= 0 {
						addIssue("forbidden_character", "incompatible", e.Path, "Windows component contains a reserved character or control code")
					}
					if reserved(c) {
						addIssue("reserved_name", "incompatible", e.Path, "Windows device name is reserved, including extensions and superscript digits")
					}
				}
				if strings.TrimRight(c, " .") != c && p.name == "windows" {
					addIssue("trailing_dot_space", "incompatible", e.Path, "Win32 shell component ends in dot or space")
				}
			}
			if p.name == "macos" || p.name == "export-fold" {
				n := norm.NFC.String(c)
				if n != c {
					causes |= causeNorm
				}
				c = n
			}
			if p.name != "posix" {
				n := fold.String(c)
				if n != c {
					causes |= causeCase
				}
				c = n
			}
			if p.name == "export-fold" {
				n := strings.Map(func(r rune) rune {
					if illegal(r) {
						return '_'
					}
					return r
				}, c)
				if n != c {
					causes |= causeReplace
				}
				c = n
			}
			if p.name == "windows" || p.name == "export-fold" {
				n := strings.TrimRight(c, " .")
				if n != c {
					causes |= causeTrim
				}
				c = n
			}
			if p.name == "export-fold" && units(c, true) > p.component {
				c = trimUnits(c, p.component)
				causes |= causeTruncate
			}
			if c == "" || c == "." || c == ".." {
				addIssue("empty_component", "incompatible", e.Path, "destination transformation produces an empty or dot component")
			}
			if p.name == "export-fold" && strings.TrimRight(c, " .") != c {
				addIssue("trailing_dot_space", "incompatible", e.Path, "truncation produces a trailing dot or space")
			}
			if p.name == "export-fold" && reserved(c) {
				addIssue("reserved_name", "incompatible", e.Path, "transformed component is a reserved Windows device name")
			}
			budgetComponent := c
			if p.name == "windows" || p.name == "macos" {
				budgetComponent = original
			}
			if i > 0 {
				budgetPath += "/"
			}
			budgetPath += budgetComponent
			if units(budgetComponent, p.utf16) > p.component {
				addIssue("component_budget", "incompatible", e.Path, fmt.Sprintf("component uses %d units; budget %d", units(budgetComponent, p.utf16), p.component))
			}
			targetPrefix += c
			kind := "directory"
			explicit := 0
			raw := sourcePrefix
			if i == len(components)-1 {
				kind = e.Kind
				explicit = 1
				raw = e.Path
			}
			key := nodeKey{sourcePrefix, kind}
			if n, ok := nodes[key]; ok {
				if explicit > 0 {
					n.addExplicit(raw)
				}
				n.causes |= inherited | causes
				n.uncertain = n.uncertain || uncertain
			} else {
				if len(nodes) >= lim.MaxNodes {
					return report, fmt.Errorf("tree node budget exceeded (%d)", lim.MaxNodes)
				}
				indexBytes += int64(128 + len(sourcePrefix) + len(targetPrefix))
				if indexBytes > lim.MaxIndexBytes {
					return report, fmt.Errorf("index byte budget exceeded (%d)", lim.MaxIndexBytes)
				}
				nodes[key] = &indexedNode{source: sourcePrefix, raw: raw, target: targetPrefix, kind: kind, explicit: explicit, causes: inherited | causes, uncertain: uncertain}
			}
			inherited |= causes
		}
		// Preserve an exact-byte identity even when Unicode transformation is
		// unavailable. This detects definite duplicate/type conflicts separately
		// from uncertain destination comparisons.
		if !utf8.ValidString(path) && p.name != "posix" {
			key := nodeKey{path, e.Kind}
			if n, ok := nodes[key]; ok {
				n.addExplicit(e.Path)
			} else {
				if len(nodes) >= lim.MaxNodes {
					return report, errors.New("tree node budget exceeded")
				}
				indexBytes += int64(128 + 2*len(path))
				if indexBytes > lim.MaxIndexBytes {
					return report, errors.New("index byte budget exceeded")
				}
				nodes[key] = &indexedNode{source: path, raw: e.Path, target: "\x00" + path, kind: e.Kind, explicit: 1, uncertain: true}
			}
		}
		joined := budgetPath
		if opts.DestinationRoot != "" {
			joined = budgetRoot + "/" + budgetPath
		}
		if (p.name == "posix" || utf8.ValidString(path)) && units(joined, p.utf16) > p.path {
			addIssue("path_budget", "incompatible", e.Path, fmt.Sprintf("destination root plus path uses %d units; budget %d", units(joined, p.utf16), p.path))
		}
	}
	report.NodeCount = len(nodes)
	index := make([]*indexedNode, 0, len(nodes))
	for _, n := range nodes {
		index = append(index, n)
	}
	sort.Slice(index, func(i, j int) bool {
		a, b := index[i], index[j]
		if a.target != b.target {
			return a.target < b.target
		}
		if a.source != b.source {
			return a.source < b.source
		}
		return a.kind < b.kind
	})
	for start := 0; start < len(index); {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		end := start + 1
		for end < len(index) && index[end].target == index[start].target {
			end++
		}
		group := index[start:end]
		conflict := len(group) > 1 || group[0].explicit > 1
		if conflict {
			var causes uint16
			status := "incompatible"
			members := make([]Member, 0, len(group))
			kinds := map[string]bool{}
			hasDir := false
			allASCII := true
			sameSource := true
			definite := false
			asciiCount := 0
			seenSources := make(map[string]bool)
			for _, n := range group {
				causes |= n.causes
				if n.explicit > 1 {
					causes |= causeDuplicate
					definite = true
				}
				if n.uncertain {
					status = "unknown"
				}
				if !ascii(n.source) {
					allASCII = false
				}
				if n.source != group[0].source {
					sameSource = false
				}
				if seenSources[n.source] {
					definite = true
				}
				seenSources[n.source] = true
				if ascii(n.source) {
					asciiCount++
				}
				kinds[n.kind] = true
				if n.kind == "directory" {
					hasDir = true
				}
				if n.spellings == nil {
					members = append(members, Member{OriginalPath(n.raw), n.kind, n.explicit == 0, n.explicit, n.uncertain})
				} else {
					spellings := make([]string, 0, len(n.spellings))
					for spelling := range n.spellings {
						spellings = append(spellings, spelling)
					}
					sort.Strings(spellings)
					for _, spelling := range spellings {
						members = append(members, Member{OriginalPath(spelling), n.kind, false, n.spellings[spelling], n.uncertain})
					}
				}
			}
			if len(kinds) > 1 && !hasDir {
				causes |= causeKind
			}
			if hasDir && len(kinds) > 1 {
				causes |= causeType
				if sameSource {
					status = "incompatible"
				}
			}
			if len(group) == 1 && group[0].explicit > 1 {
				status = "incompatible"
			}
			if hasDir && len(group) > 1 {
				causes |= causeParent
			}
			// Candidate Unicode folding cannot certify an OS collision. ASCII comparisons
			// and identical source names remain definite within the selected model.
			if allASCII || sameSource || definite || asciiCount >= 2 {
				status = "incompatible"
			}
			names := []string{}
			for bit, name := range causeNames {
				if causes&(1<<bit) != 0 {
					names = append(names, name)
				}
			}
			h := sha256.New()
			fmt.Fprintf(h, "%s\x00%s\x00%s", p.name, p.version, group[0].target)
			for _, m := range members {
				fmt.Fprintf(h, "\x00%s\x00%s\x00%d", m.Path.Base64, m.Kind, m.Count)
			}
			if len(report.Conflicts) < lim.MaxIssues {
				displayTarget := strings.ToValidUTF8(group[0].target, "\uFFFD")
				if strings.HasPrefix(displayTarget, "\x00") {
					displayTarget = "<undecodable> " + displayTarget[1:]
				}
				report.Conflicts = append(report.Conflicts, Conflict{hex.EncodeToString(h.Sum(nil))[:16], displayTarget, status, names, members})
			} else {
				truncated = true
			}
			if status == "incompatible" {
				report.Status = "incompatible"
			} else if report.Status == "known-compatible" {
				report.Status = "unknown"
			}
		}
		start = end
	}
	sort.Slice(report.Issues, func(i, j int) bool {
		a, b := report.Issues[i], report.Issues[j]
		if a.Path.Base64 != b.Path.Base64 {
			return a.Path.Base64 < b.Path.Base64
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Detail < b.Detail
	})
	// Duplicate issue messages add no information for shared ancestor components.
	out := report.Issues[:0]
	for _, v := range report.Issues {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	report.Issues = out
	if truncated {
		report.Complete = false
		report.Limitations = append(report.Limitations, "Diagnostic budget reached; report is truncated.")
		if report.Status == "known-compatible" {
			report.Status = "unknown"
		}
	}
	return report, nil
}
