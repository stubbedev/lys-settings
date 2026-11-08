package wmconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
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

// ReadOnlyError is an override file this program cannot write.
type ReadOnlyError struct {
	Path, Target string
	Err          error
}

func (e *ReadOnlyError) Error() string {
	if e.Path != e.Target {
		return fmt.Sprintf("%s is a link to %s, which is read-only; lys-wm settings cannot be changed until it is writable", e.Path, e.Target)
	}
	return e.Path + " is read-only; lys-wm settings cannot be changed until it is writable"
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

// File is lys-wm's config file, the runtime overrides this program writes over
// it, and the programs that speak for them.
type File struct {
	path    string
	runtime string
	run     Runner
}

// Open names the files lys-wm reads under this environment.
func Open(getenv func(string) string, run Runner) (*File, error) {
	dir, ok := xdg.Env(getenv).ConfigDir(compositor)
	if !ok {
		return nil, errors.New("wmconfig: neither XDG_CONFIG_HOME nor HOME is an absolute path")
	}
	return &File{path: filepath.Join(dir, "config.yaml"), runtime: filepath.Join(dir, "runtime.yaml"), run: run}, nil
}

// Path is the config file's path, the link when it is one.
func (f *File) Path() string { return f.path }

func (f *File) Runtime() string { return f.runtime }

func (f *File) tool(argv ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), toolLimit)
	defer cancel()
	return f.run(ctx, argv...)
}

// BuiltIn is the config lys-wm runs without a config file.
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

// Read is the config file's keys; false when there is no file.
func (f *File) Read() (map[string]any, bool, error) { return readTree(f.path) }

func (f *File) Overrides() (map[string]any, error) {
	tree, exists, err := readTree(f.runtime)
	if !exists && err == nil {
		tree = map[string]any{}
	}
	return tree, err
}

func readTree(path string) (map[string]any, bool, error) {
	data, err := os.ReadFile(path) //nolint:gosec // lys-wm's config and override files, as lys-wm names them
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("wmconfig: read %s: %w", path, err)
	}
	tree, err := decodeTree(data)
	if err != nil {
		return nil, true, fmt.Errorf("%s: %w", path, err)
	}
	return tree, true, nil
}

// Writable says why the override file cannot be changed, or nil.
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
	resolved := f.runtime
	info, err := os.Lstat(f.runtime)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return target{path: f.runtime, perm: 0o644, made: true}, nil
	case err != nil:
		return target{}, fmt.Errorf("wmconfig: look at %s: %w", f.runtime, err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		if resolved, err = filepath.EvalSymlinks(f.runtime); err != nil {
			return target{}, fmt.Errorf("wmconfig: %s is a link that does not resolve: %w", f.runtime, err)
		}
		if info, err = os.Stat(resolved); err != nil {
			return target{}, fmt.Errorf("wmconfig: look at %s: %w", resolved, err)
		}
	}
	if !info.Mode().IsRegular() {
		return target{}, fmt.Errorf("wmconfig: %s is not a regular file", resolved)
	}
	if info.Mode().Perm()&0o200 == 0 {
		return target{}, &ReadOnlyError{Path: f.runtime, Target: resolved, Err: fs.ErrPermission}
	}
	return target{path: resolved, perm: info.Mode().Perm()}, nil
}

// Apply makes the edits to the override file in one write after lys-wm
// accepted the result, and has a running lys-wm reload it. A refused or
// impossible change leaves the file as it was; the config file is only read.
func (f *File) Apply(edits []Edit) (Reload, error) {
	for range maxAttempts {
		reload, retry, err := f.attempt(edits)
		if !retry {
			return reload, err
		}
	}
	return Unchanged, fmt.Errorf("wmconfig: %s keeps changing; try again", f.runtime)
}

func (f *File) ResetAll() (Reload, error) {
	over, err := f.Overrides()
	if err != nil {
		return Unchanged, err
	}
	return f.Apply(ResetEdits(over))
}

func ResetEdits(over map[string]any) []Edit {
	edits := make([]Edit, 0, len(over))
	for _, key := range slices.Sorted(maps.Keys(over)) {
		edits = append(edits, Edit{Path: []string{key}, Unset: true})
	}
	return edits
}

func (f *File) attempt(edits []Edit) (Reload, bool, error) {
	t, err := f.target()
	if err != nil {
		return Unchanged, false, err
	}
	current, err := f.current(t)
	if err != nil {
		return Unchanged, existenceChanged(err), err
	}
	next, err := applyEdits(current, edits)
	if err != nil {
		return Unchanged, false, err
	}
	if bytes.Equal(next, current) {
		return Unchanged, false, nil
	}
	if err := f.check(t, next); err != nil {
		return Unchanged, false, err
	}
	now, err := f.current(t)
	if existenceChanged(err) || err == nil && !bytes.Equal(now, current) {
		return Unchanged, true, nil
	}
	if err != nil {
		return Unchanged, false, err
	}
	if err := atomicfile.WriteFile(t.path, next, t.perm); err != nil {
		return Unchanged, false, readOnly(f.runtime, t.path, fmt.Errorf("wmconfig: write %s: %w", t.path, err))
	}
	reload, err := f.reload()
	return reload, false, err
}

// Check reports why lys-wm would refuse the override file with the edits
// made, or nil when it would take it; nothing is written.
func (f *File) Check(edits []Edit) error {
	t, err := f.target()
	if err != nil {
		return err
	}
	current, err := f.current(t)
	if err != nil {
		return err
	}
	next, err := applyEdits(current, edits)
	if err != nil {
		return err
	}
	return f.check(t, next)
}

func applyEdits(src []byte, edits []Edit) ([]byte, error) {
	var err error
	for _, e := range edits {
		if src, err = ApplyEdit(src, e); err != nil {
			return nil, err
		}
	}
	return src, nil
}

func (f *File) current(t target) ([]byte, error) {
	data, err := os.ReadFile(t.path)
	switch {
	case t.made && errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case t.made && err == nil:
		return nil, fs.ErrExist
	case err != nil:
		return nil, readOnly(f.runtime, t.path, fmt.Errorf("wmconfig: read %s: %w", t.path, err))
	}
	return data, nil
}

func existenceChanged(err error) bool {
	return errors.Is(err, fs.ErrExist) || errors.Is(err, fs.ErrNotExist)
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
		return readOnly(f.runtime, t.path, fmt.Errorf("wmconfig: make %s: %w", dir, err))
	}
	tmp, err := os.CreateTemp(dir, ".runtime.yaml.check-*")
	if err != nil {
		return readOnly(f.runtime, t.path, fmt.Errorf("wmconfig: write beside %s: %w", t.path, err))
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = tmp.Write(data)
	if err = errors.Join(err, tmp.Close()); err != nil {
		return fmt.Errorf("wmconfig: write %s: %w", tmp.Name(), err)
	}
	argv := []string{compositor, "--check-config", "--runtime", tmp.Name()}
	if _, err := os.Stat(f.path); err == nil {
		argv = append(argv, "--config", f.path)
	}
	_, err = f.tool(argv...)
	if err == nil {
		return nil
	}
	var tool *ToolError
	if errors.As(err, &tool) && tool.Exit > 0 {
		return &RejectedError{Reason: strings.ReplaceAll(tool.Stderr, tmp.Name(), f.runtime)}
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

func Merge(base, over map[string]any) map[string]any {
	out := maps.Clone(base)
	if out == nil {
		out = map[string]any{}
	}
	for key, v := range over {
		b, bok := out[key].(map[string]any)
		o, ook := v.(map[string]any)
		if bok && ook {
			out[key] = Merge(b, o)
			continue
		}
		out[key] = v
	}
	return out
}

func Overridden(over map[string]any, path []string) bool {
	node := over
	for i, key := range path {
		v, ok := node[key]
		if !ok {
			return false
		}
		child, isMap := v.(map[string]any)
		if i == len(path)-1 || !isMap {
			return true
		}
		node = child
	}
	return false
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
	// Switch is false, the key off, or an object of the keys Keys lists.
	Switch
)

// Field is one key of the schema.
type Field struct {
	Kind   Kind
	Format string
	// Variants are the values an Enum takes: strings and booleans, as the file
	// writes them.
	Variants []any
	Min, Max float64
	// MinItems and MaxItems bound how many items a list takes.
	MinItems, MaxItems int
	Default            any
	HasDefault         bool
	Actions            []Action
}

type Action struct {
	Name    string `json:"name"`
	Args    string `json:"args"`
	Summary string `json:"summary"`
}

// Schema is lys-wm's config schema, parsed at the boundary.
type Schema struct{ root *schemaNode }

type schemaNode struct {
	Type             string                 `json:"type"`
	Format           string                 `json:"format"`
	Enum             variants               `json:"enum"`
	Minimum          *float64               `json:"minimum"`
	ExclusiveMinimum *float64               `json:"exclusiveMinimum"`
	Maximum          *float64               `json:"maximum"`
	MinItems         *int                   `json:"minItems"`
	MaxItems         *int                   `json:"maxItems"`
	Default          *json.RawMessage       `json:"default"`
	OneOf            []*schemaNode          `json:"oneOf"`
	Properties       map[string]*schemaNode `json:"properties"`
	Items            *schemaNode            `json:"items"`
	Actions          []Action               `json:"x-lys-wm-actions"`
	// Values is a mapping's additionalProperties when it is a schema; the
	// JSON boolean form, which says only whether other keys are allowed,
	// leaves it nil.
	Values *schemaNode `json:"-"`
}

// UnmarshalJSON reads additionalProperties, which is a boolean or a schema.
func (n *schemaNode) UnmarshalJSON(data []byte) error {
	type plain schemaNode
	var raw struct {
		*plain
		Additional json.RawMessage `json:"additionalProperties"`
	}
	raw.plain = (*plain)(n)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.Additional) == 0 || raw.Additional[0] != '{' {
		return nil
	}
	n.Values = &schemaNode{}
	return json.Unmarshal(raw.Additional, n.Values)
}

type variants []any

// UnmarshalJSON reads an enum, refusing a value no editor could offer.
func (v *variants) UnmarshalJSON(data []byte) error {
	var raw []any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for _, x := range raw {
		switch x.(type) {
		case string, bool:
		default:
			return fmt.Errorf("enum value %v is neither a string nor a boolean", x)
		}
	}
	*v = raw
	return nil
}

// Each is the path segment naming every item of a list or every value of a
// mapping, as in outputs.[].scale.
const Each = "[]"

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
	node, ok := s.node(path)
	if !ok {
		return Field{}, false
	}
	return node.field()
}

// Keys lists, sorted, the keys of the object at path, or of a Switch's object
// form; false when path names no object with keys of its own.
func (s *Schema) Keys(path []string) ([]string, bool) {
	node, ok := s.node(path)
	if !ok {
		return nil, false
	}
	if form := node.switched(); form != nil {
		node = form
	}
	if len(node.Properties) == 0 {
		return nil, false
	}
	return slices.Sorted(maps.Keys(node.Properties)), true
}

func (s *Schema) node(path []string) (*schemaNode, bool) {
	node := s.root
	for _, key := range path {
		var child *schemaNode
		switch {
		case key == Each && node.Items != nil:
			child = node.Items
		case key == Each:
			child = node.Values
		default:
			child = node.property(key)
		}
		if child == nil {
			return nil, false
		}
		node = child
	}
	return node, true
}

func (n *schemaNode) field() (Field, bool) {
	f := Field{Min: -unboundedInt, Max: unboundedInt, MaxItems: unboundedInt, Format: n.Format, Actions: slices.Clone(n.Actions)}
	switch {
	case n.switched() != nil:
		f.Kind = Switch
	case len(n.OneOf) > 0 || n.Type == "object" || n.Type == "array":
		f.Kind = Composite
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
	if n.MinItems != nil {
		f.MinItems = *n.MinItems
	}
	if n.MaxItems != nil {
		f.MaxItems = *n.MaxItems
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
// int64 and the others as float64, whichever form the file wrote, and a
// list or mapping as the file's decoder reads it.
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
	case Composite, Switch:
		if out, err := normal(v); err == nil {
			return out
		}
	case Bool, Enum:
	}
	return v
}

// switched is the object form of a key that is false or an object, nil for
// any other key.
func (n *schemaNode) switched() *schemaNode {
	if len(n.OneOf) != 2 {
		return nil
	}
	off, on := n.OneOf[0], n.OneOf[1]
	if off.Type != "boolean" || !slices.Equal([]any(off.Enum), []any{false}) || on.Type != "object" || len(on.Properties) == 0 {
		return nil
	}
	return on
}

// property is the key of an object, or of the object form of a key that
// takes either of two forms.
func (n *schemaNode) property(key string) *schemaNode {
	if child, ok := n.Properties[key]; ok {
		return child
	}
	for _, form := range n.OneOf {
		if child, ok := form.Properties[key]; ok {
			return child
		}
	}
	return nil
}
