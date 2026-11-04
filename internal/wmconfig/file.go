package wmconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/stubbedev/lys-lib/atomicfile"
	"github.com/stubbedev/lys-lib/xdg"
)

const (
	compositor = "lys-wm"
	control    = "lys-wmctl"
	notRunning = 3
	toolLimit  = 10 * time.Second
	// A file edited by hand between the read and the write is read again.
	maxAttempts = 3
)

// Installed reports whether lys-wm is on the path. Without it nothing can check
// a change, so there is nothing to offer.
func Installed() bool {
	_, err := exec.LookPath(compositor)
	return err == nil
}

// ToolError is a program that failed: what it was asked, and what it said.
type ToolError struct {
	Argv   []string
	Exit   int
	Stderr string
	Err    error
}

func (e *ToolError) Error() string {
	if e.Stderr != "" {
		return e.Stderr
	}
	return fmt.Sprintf("%s: %v", strings.Join(e.Argv, " "), e.Err)
}

func (e *ToolError) Unwrap() error { return e.Err }

// Runner runs a program to its end; a failure is a *ToolError.
type Runner func(ctx context.Context, argv ...string) ([]byte, error)

// Exec is the Runner that starts the program.
func Exec(ctx context.Context, argv ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //nolint:gosec // argv is built from the two lys-wm programs and a path, never through a shell
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	code := -1
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		code = exit.ExitCode()
	}
	return nil, &ToolError{Argv: argv, Exit: code, Stderr: strings.TrimSpace(stderr.String()), Err: err}
}

// ReadOnlyError is a config this program must not write, usually Home Manager's
// link into the Nix store: its owner has to change the setting.
type ReadOnlyError struct {
	Path, Target string
	Err          error
}

func (e *ReadOnlyError) Error() string {
	if e.Path != e.Target {
		return fmt.Sprintf("%s is a link to %s, which is read-only; if Home Manager or Nix writes it, change the setting there", e.Path, e.Target)
	}
	return e.Path + " is read-only"
}

func (e *ReadOnlyError) Unwrap() error { return e.Err }

// RejectedError is a change lys-wm refused; the file was not touched.
type RejectedError struct{ Reason string }

func (e *RejectedError) Error() string { return "lys-wm refused the change: " + e.Reason }

// Reload is what happened after a change reached the file.
type Reload int

const (
	// Unchanged means the file already said so, and lys-wm was not asked.
	Unchanged Reload = iota
	// Reloaded means the running lys-wm took the change.
	Reloaded
	// NotRunning means no lys-wm answered: the next one reads the change.
	NotRunning
)

// ReloadError is a change that was written but that lys-wm did not take.
type ReloadError struct{ Reason string }

func (e *ReloadError) Error() string {
	return "the change was saved, but lys-wm did not reload it: " + e.Reason
}

// File is lys-wm's config file and the programs that speak for it.
type File struct {
	path string
	run  Runner
}

// Open names the file lys-wm reads under this environment.
func Open(getenv func(string) string, run Runner) (*File, error) {
	dir, ok := xdg.Env(getenv).ConfigDir(compositor)
	if !ok {
		return nil, errors.New("wmconfig: neither XDG_CONFIG_HOME nor HOME is an absolute path")
	}
	return &File{path: filepath.Join(dir, "config.yaml"), run: run}, nil
}

// Path is the file's path, the link when it is one.
func (f *File) Path() string { return f.path }

func (f *File) tool(argv ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), toolLimit)
	defer cancel()
	return f.run(ctx, argv...)
}

// BuiltIn is the config lys-wm runs without a file, which a new file has to
// start from: a file replaces it whole.
func (f *File) BuiltIn() ([]byte, error) {
	out, err := f.tool(compositor, "--default-config")
	if err != nil {
		return nil, fmt.Errorf("wmconfig: ask lys-wm for its built-in config: %w", err)
	}
	return out, nil
}

// Schema is the installed lys-wm's description of its config.
func (f *File) Schema() (*Schema, error) {
	out, err := f.tool(compositor, "--config-schema")
	if err != nil {
		return nil, fmt.Errorf("wmconfig: ask lys-wm for its config schema: %w", err)
	}
	return ParseSchema(out)
}

// Read is the file's keys; false when there is no file.
func (f *File) Read() (map[string]any, bool, error) {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("wmconfig: read %s: %w", f.path, err)
	}
	tree, err := decodeTree(data)
	if err != nil {
		return nil, true, fmt.Errorf("%s: %w", f.path, err)
	}
	return tree, true, nil
}

// Writable says why the file cannot be changed, or nil.
func (f *File) Writable() error {
	_, err := f.target()
	return err
}

type target struct {
	path string
	perm fs.FileMode
	made bool
}

func (f *File) target() (target, error) {
	resolved := f.path
	info, err := os.Lstat(f.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return target{path: f.path, perm: 0o644, made: true}, nil
	case err != nil:
		return target{}, fmt.Errorf("wmconfig: look at %s: %w", f.path, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		if resolved, err = filepath.EvalSymlinks(f.path); err != nil {
			return target{}, fmt.Errorf("wmconfig: %s is a link that does not resolve: %w", f.path, err)
		}
		if info, err = os.Stat(resolved); err != nil {
			return target{}, fmt.Errorf("wmconfig: look at %s: %w", resolved, err)
		}
	}
	if !info.Mode().IsRegular() {
		return target{}, fmt.Errorf("wmconfig: %s is not a regular file", resolved)
	}
	if info.Mode().Perm()&0o200 == 0 {
		return target{}, &ReadOnlyError{Path: f.path, Target: resolved, Err: fs.ErrPermission}
	}
	return target{path: resolved, perm: info.Mode().Perm()}, nil
}

// Apply makes the edits in one write after lys-wm accepted the result, and has
// a running lys-wm reload it. A refused or impossible change leaves the file as
// it was.
func (f *File) Apply(edits []Edit) (Reload, error) {
	for range maxAttempts {
		reload, retry, err := f.attempt(edits)
		if !retry {
			return reload, err
		}
	}
	return Unchanged, fmt.Errorf("wmconfig: %s keeps changing; try again", f.path)
}

func (f *File) attempt(edits []Edit) (Reload, bool, error) {
	t, err := f.target()
	if err != nil {
		return Unchanged, false, err
	}
	current, err := f.current(t)
	if err != nil {
		return Unchanged, false, err
	}
	next := current
	for _, e := range edits {
		if next, err = ApplyEdit(next, e); err != nil {
			return Unchanged, false, err
		}
	}
	if !t.made && bytes.Equal(next, current) {
		return Unchanged, false, nil
	}
	if err := f.check(t, next); err != nil {
		return Unchanged, false, err
	}
	if !t.made {
		now, err := os.ReadFile(t.path)
		if err != nil {
			return Unchanged, false, fmt.Errorf("wmconfig: read %s: %w", t.path, err)
		}
		if !bytes.Equal(now, current) {
			return Unchanged, true, nil
		}
	}
	if err := atomicfile.WriteFile(t.path, next, t.perm); err != nil {
		return Unchanged, false, readOnly(f.path, t.path, fmt.Errorf("wmconfig: write %s: %w", t.path, err))
	}
	reload, err := f.reload()
	return reload, false, err
}

func (f *File) current(t target) ([]byte, error) {
	if t.made {
		return f.BuiltIn()
	}
	data, err := os.ReadFile(t.path)
	if err != nil {
		return nil, readOnly(f.path, t.path, fmt.Errorf("wmconfig: read %s: %w", t.path, err))
	}
	return data, nil
}

func readOnly(link, path string, err error) error {
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS) {
		return &ReadOnlyError{Path: link, Target: path, Err: err}
	}
	return err
}

func (f *File) check(t target, data []byte) error {
	dir := filepath.Dir(t.path)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // the user's own config directory, as lys-wm documents it
		return readOnly(f.path, t.path, fmt.Errorf("wmconfig: make %s: %w", dir, err))
	}
	tmp, err := os.CreateTemp(dir, ".config.yaml.check-*")
	if err != nil {
		return readOnly(f.path, t.path, fmt.Errorf("wmconfig: write beside %s: %w", t.path, err))
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = tmp.Write(data)
	if err = errors.Join(err, tmp.Close()); err != nil {
		return fmt.Errorf("wmconfig: write %s: %w", tmp.Name(), err)
	}
	_, err = f.tool(compositor, "--check-config", "--config", tmp.Name())
	if err == nil {
		return nil
	}
	var tool *ToolError
	if errors.As(err, &tool) && tool.Exit > 0 {
		return &RejectedError{Reason: strings.ReplaceAll(tool.Stderr, tmp.Name(), t.path)}
	}
	return fmt.Errorf("wmconfig: have lys-wm check the change: %w", err)
}

func (f *File) reload() (Reload, error) {
	_, err := f.tool(control, "reload")
	if err == nil {
		return Reloaded, nil
	}
	var tool *ToolError
	if errors.As(err, &tool) && tool.Exit == notRunning {
		return NotRunning, nil
	}
	return Unchanged, &ReloadError{Reason: err.Error()}
}

// Lookup is the value at path in a tree of keys.
func Lookup(tree map[string]any, path []string) (any, bool) {
	var node any = tree
	for _, key := range path {
		m, ok := node.(map[string]any)
		if !ok {
			return nil, false
		}
		if node, ok = m[key]; !ok {
			return nil, false
		}
	}
	return node, true
}

// Tree parses a config's bytes into its keys.
func Tree(data []byte) (map[string]any, error) { return decodeTree(data) }

// Kind is what a key holds.
type Kind int

const (
	// Composite is a list, a mapping, or a key taking either of two forms.
	Composite Kind = iota
	Bool
	Int
	Float
	Text
	Enum
)

// Field is one key of the schema.
type Field struct {
	Kind       Kind
	Variants   []string
	Min, Max   float64
	Default    any
	HasDefault bool
}

// Schema is lys-wm's config schema, parsed at the boundary.
type Schema struct{ root *schemaNode }

type schemaNode struct {
	Type             string                 `json:"type"`
	Format           string                 `json:"format"`
	Enum             []string               `json:"enum"`
	Minimum          *float64               `json:"minimum"`
	ExclusiveMinimum *float64               `json:"exclusiveMinimum"`
	Maximum          *float64               `json:"maximum"`
	Default          *json.RawMessage       `json:"default"`
	OneOf            []*schemaNode          `json:"oneOf"`
	Properties       map[string]*schemaNode `json:"properties"`
}

// ParseSchema reads what lys-wm --config-schema prints.
func ParseSchema(data []byte) (*Schema, error) {
	var root schemaNode
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("wmconfig: read lys-wm's config schema: %w", err)
	}
	if root.Type != "object" || len(root.Properties) == 0 {
		return nil, errors.New("wmconfig: lys-wm's config schema describes no keys")
	}
	return &Schema{root: &root}, nil
}

const (
	unboundedInt   = 1<<31 - 1
	unboundedFloat = 1e6
	// exclusiveStep stands in for an open lower bound in a control that takes
	// closed ones.
	exclusiveStep = 0.01
)

// Field finds the key at path; false when this lys-wm has no such key.
func (s *Schema) Field(path []string) (Field, bool) {
	node := s.root
	for _, key := range path {
		child, ok := node.Properties[key]
		if !ok {
			return Field{}, false
		}
		node = child
	}
	return node.field()
}

func (n *schemaNode) field() (Field, bool) {
	f := Field{Min: -unboundedInt, Max: unboundedInt}
	switch {
	case len(n.OneOf) > 0 || n.Type == "object" || n.Type == "array":
		return Field{Kind: Composite}, true
	case len(n.Enum) > 0:
		f.Kind, f.Variants = Enum, slices.Clone(n.Enum)
	case n.Type == "boolean":
		f.Kind = Bool
	case n.Type == "integer":
		f.Kind = Int
	case n.Type == "number":
		f.Kind, f.Min, f.Max = Float, -unboundedFloat, unboundedFloat
	case n.Type == "string":
		f.Kind = Text
	default:
		return Field{}, false
	}
	if n.Minimum != nil {
		f.Min = *n.Minimum
	}
	if n.ExclusiveMinimum != nil {
		f.Min = *n.ExclusiveMinimum + exclusiveStep
	}
	if n.Maximum != nil {
		f.Max = *n.Maximum
	}
	if n.Default != nil {
		var v any
		if err := json.Unmarshal(*n.Default, &v); err != nil {
			return Field{}, false
		}
		f.Default, f.HasDefault = f.Normal(v), true
	}
	return f, true
}

// Normal is a value as an editor of this key expects it: whole numbers as
// int64 and the others as float64, whichever form the file wrote.
func (f Field) Normal(v any) any {
	switch f.Kind {
	case Int:
		switch n := v.(type) {
		case int:
			return int64(n)
		case float64:
			return int64(n)
		}
	case Float:
		switch n := v.(type) {
		case int:
			return float64(n)
		case int64:
			return float64(n)
		}
	case Text:
		switch n := v.(type) {
		case int:
			return strconv.Itoa(n)
		case float64:
			return strconv.FormatFloat(n, 'g', -1, 64)
		}
	case Composite, Bool, Enum:
	}
	return v
}
