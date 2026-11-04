// Package wmconfig reads and edits lys-wm's config file. Edits touch only
// the bytes of the value they change, so comments, blank lines, key order and
// keys this program does not know survive, and each edit is held to what it
// meant by parsing the result before anything is written.
package wmconfig

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// Edit changes one key: Unset removes it, otherwise it takes Value.
type Edit struct {
	Path  []string
	Value any
	Unset bool
}

func (e Edit) String() string { return strings.Join(e.Path, ".") }

// ApplyEdit is src with the edit made, or an error naming the line of what it
// cannot safely change; src is never partly edited.
func ApplyEdit(src []byte, e Edit) ([]byte, error) {
	if len(e.Path) == 0 {
		return nil, errors.New("wmconfig: an edit needs a key")
	}
	before, err := decodeTree(src)
	if err != nil {
		return nil, err
	}
	var out []byte
	if e.Unset {
		out, err = unset(src, e.Path)
	} else {
		out, err = set(src, e.Path, e.Value)
	}
	if err != nil {
		return nil, fmt.Errorf("wmconfig: edit %s: %w", e, err)
	}
	want, err := edited(before, e)
	if err != nil {
		return nil, fmt.Errorf("wmconfig: edit %s: %w", e, err)
	}
	got, err := decodeTree(out)
	if err != nil {
		return nil, fmt.Errorf("wmconfig: edit %s left a file that does not parse: %w", e, err)
	}
	if !reflect.DeepEqual(got, want) {
		return nil, fmt.Errorf("wmconfig: edit %s would change more than that key; change it by hand", e)
	}
	return out, nil
}

func decodeTree(src []byte) (map[string]any, error) {
	var tree map[string]any
	if err := yaml.Unmarshal(src, &tree); err != nil {
		return nil, fmt.Errorf("wmconfig: the config does not parse: %w", err)
	}
	if tree == nil {
		tree = map[string]any{}
	}
	return tree, nil
}

func edited(tree map[string]any, e Edit) (map[string]any, error) {
	if e.Unset {
		return pruned(tree, e.Path), nil
	}
	value, err := normal(e.Value)
	if err != nil {
		return nil, err
	}
	return with(tree, e.Path, value), nil
}

func normal(v any) (any, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode the value: %w", err)
	}
	var out any
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("read the value back: %w", err)
	}
	return out, nil
}

func with(tree map[string]any, path []string, v any) map[string]any {
	out := make(map[string]any, len(tree)+1)
	maps.Copy(out, tree)
	if len(path) == 1 {
		out[path[0]] = v
		return out
	}
	child, _ := tree[path[0]].(map[string]any)
	out[path[0]] = with(child, path[1:], v)
	return out
}

func pruned(tree map[string]any, path []string) map[string]any {
	out := make(map[string]any, len(tree))
	maps.Copy(out, tree)
	if len(path) == 1 {
		delete(out, path[0])
		return out
	}
	child, ok := tree[path[0]].(map[string]any)
	if !ok {
		return out
	}
	rest := pruned(child, path[1:])
	if len(rest) == 0 && len(child) > 0 {
		delete(out, path[0])
		return out
	}
	out[path[0]] = rest
	return out
}

type document struct {
	src   []byte
	lines []int
	root  *yaml.Node
}

func parse(src []byte) (*document, error) {
	var n yaml.Node
	if err := yaml.Unmarshal(src, &n); err != nil {
		return nil, fmt.Errorf("the config does not parse: %w", err)
	}
	d := &document{src: src, lines: lineStarts(src)}
	if n.Kind != yaml.DocumentNode || len(n.Content) == 0 {
		return d, nil
	}
	root := n.Content[0]
	if isNull(root) {
		return d, nil
	}
	d.root = root
	return d, nil
}

func lineStarts(src []byte) []int {
	lines := []int{0}
	for i := 0; i < len(src); i += breakSize(src, i) {
		if isBreak(src, i) {
			lines = append(lines, i+breakSize(src, i))
		}
	}
	return lines
}

func isBreak(src []byte, i int) bool {
	switch src[i] {
	case '\n', '\r':
		return true
	case 0xc2:
		return i+1 < len(src) && src[i+1] == 0x85
	case 0xe2:
		return i+2 < len(src) && src[i+1] == 0x80 && (src[i+2] == 0xa8 || src[i+2] == 0xa9)
	}
	return false
}

func breakSize(src []byte, i int) int {
	if !isBreak(src, i) {
		return 1
	}
	switch src[i] {
	case '\r':
		if i+1 < len(src) && src[i+1] == '\n' {
			return 2
		}
		return 1
	case '\n':
		return 1
	case 0xc2:
		return 2
	}
	return 3
}

func isNull(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag == "!!null" }

func isFlow(n *yaml.Node) bool { return n.Style&yaml.FlowStyle != 0 }

func (d *document) lineStart(line int) int {
	if line > len(d.lines) {
		return len(d.src)
	}
	return d.lines[line-1]
}

func (d *document) offset(line, column int) int {
	at := d.lineStart(line)
	for range column - 1 {
		_, size := utf8.DecodeRune(d.src[at:])
		at += size
	}
	return at
}

func (d *document) lineEnd(at int) int {
	for i := at; i < len(d.src); i++ {
		if isBreak(d.src, i) {
			return i
		}
	}
	return len(d.src)
}

func indentAt(src []byte, lineStart int) int {
	n := 0
	for lineStart+n < len(src) && src[lineStart+n] == ' ' {
		n++
	}
	return n
}

type pair struct{ key, value *yaml.Node }

func lookup(m *yaml.Node, key string) (pair, bool, error) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if k.Value == "<<" {
			return pair{}, false, fmt.Errorf("line %d merges a mapping into this one", k.Line)
		}
		if k.Value == key {
			return pair{k, v}, true, nil
		}
	}
	return pair{}, false, nil
}

func (d *document) colonAfter(key *yaml.Node, flow bool) (int, error) {
	end, err := d.scalarEnd(key, flow, -1)
	if err != nil {
		return 0, err
	}
	i := end
	for i < len(d.src) && (d.src[i] == ' ' || d.src[i] == '\t') {
		i++
	}
	if i >= len(d.src) || d.src[i] != ':' {
		return 0, fmt.Errorf("line %d: no colon after the key %q", key.Line, key.Value)
	}
	return i + 1, nil
}

func (d *document) scalarEnd(n *yaml.Node, flow bool, parentIndent int) (int, error) {
	if n.Line == 0 {
		return 0, errors.New("a value has no position")
	}
	start := d.offset(n.Line, n.Column)
	if start >= len(d.src) {
		return start, nil
	}
	switch d.src[start] {
	case '"':
		for i := start + 1; i < len(d.src); i++ {
			switch d.src[i] {
			case '\\':
				i++
			case '"':
				return i + 1, nil
			}
		}
		return 0, fmt.Errorf("line %d: an unterminated string", n.Line)
	case '\'':
		for i := start + 1; i < len(d.src); i++ {
			if d.src[i] != '\'' {
				continue
			}
			if i+1 < len(d.src) && d.src[i+1] == '\'' {
				i++
				continue
			}
			return i + 1, nil
		}
		return 0, fmt.Errorf("line %d: an unterminated string", n.Line)
	case '|', '>':
		return d.continuation(d.lineEnd(start), parentIndent), nil
	}
	end := d.plainEnd(start, flow)
	if flow || parentIndent < 0 {
		return end, nil
	}
	if rest := bytes.TrimSpace(d.src[end:d.lineEnd(end)]); len(rest) > 0 {
		return end, nil
	}
	return d.continuation(end, parentIndent), nil
}

func (d *document) plainEnd(start int, flow bool) int {
	stop := d.lineEnd(start)
	for i := start; i < stop; i++ {
		switch b := d.src[i]; {
		case b == '#' && i > start && (d.src[i-1] == ' ' || d.src[i-1] == '\t'):
			stop = i
		case b == ':' && (i+1 == len(d.src) || d.src[i+1] == ' ' || isBreak(d.src, i+1)):
			stop = i
		case flow && (b == ',' || b == '}' || b == ']'):
			stop = i
		}
	}
	return start + len(bytes.TrimRight(d.src[start:stop], " \t"))
}

func (d *document) continuation(end, parentIndent int) int {
	for {
		lineEnd := d.lineEnd(end)
		if lineEnd >= len(d.src) {
			return end
		}
		next := lineEnd + breakSize(d.src, lineEnd)
		if next >= len(d.src) {
			return end
		}
		stop := d.lineEnd(next)
		line := d.src[next:stop]
		if len(bytes.TrimSpace(line)) == 0 {
			probe := d.continuation(stop, parentIndent)
			if probe == stop {
				return end
			}
			end = probe
			continue
		}
		if indentAt(d.src, next) <= parentIndent {
			return end
		}
		end = stop
	}
}

func (d *document) nodeEnd(n *yaml.Node, flow bool, parentIndent int) (int, error) {
	switch n.Kind {
	case yaml.ScalarNode, yaml.AliasNode:
		return d.scalarEnd(n, flow, parentIndent)
	case yaml.MappingNode, yaml.SequenceNode:
		if isFlow(n) {
			return d.flowEnd(n)
		}
		if len(n.Content) == 0 {
			return 0, fmt.Errorf("line %d: an empty block value", n.Line)
		}
		return d.nodeEnd(n.Content[len(n.Content)-1], false, n.Column-1)
	}
	return 0, fmt.Errorf("line %d: a value this program cannot read", n.Line)
}

func (d *document) flowEnd(n *yaml.Node) (int, error) {
	from := d.offset(n.Line, n.Column) + 1
	if len(n.Content) > 0 {
		last, err := d.nodeEnd(n.Content[len(n.Content)-1], true, -1)
		if err != nil {
			return 0, err
		}
		from = last
	}
	closer := byte('}')
	if n.Kind == yaml.SequenceNode {
		closer = ']'
	}
	for i := from; i < len(d.src); i++ {
		switch b := d.src[i]; b {
		case closer:
			return i + 1, nil
		case '#':
			i = d.lineEnd(i)
		case ' ', '\t', '\n', '\r', ',':
		default:
			return 0, fmt.Errorf("line %d: a flow value this program cannot read", n.Line)
		}
	}
	return 0, fmt.Errorf("line %d: an unterminated flow value", n.Line)
}

func render(v any, flow bool, indent int) (string, bool, error) {
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		return "", false, fmt.Errorf("encode the value: %w", err)
	}
	if flow {
		setFlow(&n)
	}
	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&n); err != nil {
		return "", false, fmt.Errorf("encode the value: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", false, fmt.Errorf("encode the value: %w", err)
	}
	text := strings.TrimRight(out.String(), "\n")
	if flow || !strings.Contains(text, "\n") && n.Kind == yaml.ScalarNode {
		return text, false, nil
	}
	if n.Kind != yaml.ScalarNode && len(n.Content) == 0 {
		return text, false, nil
	}
	pad := strings.Repeat(" ", indent)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = pad + line
	}
	return strings.Join(lines, "\n"), true, nil
}

func setFlow(n *yaml.Node) {
	if n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode {
		n.Style = yaml.FlowStyle
	}
	for _, c := range n.Content {
		setFlow(c)
	}
}

func nest(path []string, v any) any {
	for _, key := range slices.Backward(path) {
		v = map[string]any{key: v}
	}
	return v
}

func splice(src []byte, from, to int, text string) []byte {
	out := make([]byte, 0, len(src)-(to-from)+len(text))
	out = append(out, src[:from]...)
	out = append(out, text...)
	return append(out, src[to:]...)
}

func set(src []byte, path []string, value any) ([]byte, error) {
	d, err := parse(src)
	if err != nil {
		return nil, err
	}
	if d.root == nil || len(d.root.Content) == 0 && !isFlow(d.root) {
		return d.appendRoot(path, value)
	}
	parent := d.root
	indent := d.root.Column - 1
	for i, key := range path {
		p, found, err := lookup(parent, key)
		if err != nil {
			return nil, err
		}
		if !found {
			return d.insert(parent, indent, path[i:], value)
		}
		if p.value.Anchor != "" {
			return nil, fmt.Errorf("line %d: %s is an anchor other keys may use; change it by hand", p.key.Line, key)
		}
		last := i == len(path)-1
		if last || isNull(p.value) || p.value.Kind == yaml.AliasNode || p.value.Kind != yaml.MappingNode {
			if !last && !isNull(p.value) {
				return nil, fmt.Errorf("line %d: %s is not a mapping", p.key.Line, key)
			}
			if p.value.Kind == yaml.AliasNode {
				return nil, fmt.Errorf("line %d: %s is an alias; change it by hand", p.key.Line, key)
			}
			return d.replace(p, isFlow(parent), indent, nest(path[i+1:], value))
		}
		parent = p.value
		indent = parent.Column - 1
	}
	return nil, errors.New("an edit needs a key")
}

func (d *document) appendRoot(path []string, value any) ([]byte, error) {
	text, _, err := render(nest(path, value), false, 0)
	if err != nil {
		return nil, err
	}
	out := slices.Clone(d.src)
	if len(out) > 0 && out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return append(append(out, text...), '\n'), nil
}

func (d *document) replace(p pair, flow bool, keyIndent int, value any) ([]byte, error) {
	colon, err := d.colonAfter(p.key, flow)
	if err != nil {
		return nil, err
	}
	end := colon
	if !isEmptyValue(p.value) {
		if end, err = d.nodeEnd(p.value, flow, keyIndent); err != nil {
			return nil, err
		}
	}
	text, block, err := render(value, flow, keyIndent+2)
	if err != nil {
		return nil, err
	}
	if block {
		return splice(d.src, colon, end, "\n"+text), nil
	}
	return splice(d.src, colon, end, " "+text), nil
}

func isEmptyValue(n *yaml.Node) bool { return isNull(n) && n.Value == "" }

func (d *document) insert(parent *yaml.Node, indent int, path []string, value any) ([]byte, error) {
	v := nest(path[1:], value)
	if isFlow(parent) {
		return d.insertFlow(parent, path[0], v)
	}
	last := parent.Content[len(parent.Content)-1]
	end, err := d.nodeEnd(last, false, indent)
	if err != nil {
		return nil, err
	}
	if isEmptyValue(last) {
		key := parent.Content[len(parent.Content)-2]
		if end, err = d.colonAfter(key, false); err != nil {
			return nil, err
		}
	}
	text, _, err := render(map[string]any{path[0]: v}, false, indent)
	if err != nil {
		return nil, err
	}
	at := d.lineEnd(end)
	out := splice(d.src, at, at, "\n"+text)
	return out, nil
}

func (d *document) insertFlow(parent *yaml.Node, key string, v any) ([]byte, error) {
	text, _, err := render(map[string]any{key: v}, true, 0)
	if err != nil {
		return nil, err
	}
	text = strings.TrimSuffix(strings.TrimPrefix(text, "{"), "}")
	if len(parent.Content) == 0 {
		end, err := d.flowEnd(parent)
		if err != nil {
			return nil, err
		}
		return splice(d.src, end-1, end-1, text), nil
	}
	last, err := d.nodeEnd(parent.Content[len(parent.Content)-1], true, -1)
	if err != nil {
		return nil, err
	}
	return splice(d.src, last, last, ", "+text), nil
}

func unset(src []byte, path []string) ([]byte, error) {
	d, err := parse(src)
	if err != nil {
		return nil, err
	}
	if d.root == nil {
		return src, nil
	}
	chain := make([]*yaml.Node, 0, len(path))
	parent := d.root
	for i, key := range path {
		chain = append(chain, parent)
		p, found, err := lookup(parent, key)
		if err != nil {
			return nil, err
		}
		if !found {
			return src, nil
		}
		if i == len(path)-1 {
			break
		}
		if p.value.Kind != yaml.MappingNode {
			return src, nil
		}
		parent = p.value
	}
	drop := len(path) - 1
	for drop > 0 && len(chain[drop].Content) == 2 {
		drop--
	}
	p, _, err := lookup(chain[drop], path[drop])
	if err != nil {
		return nil, err
	}
	if isFlow(chain[drop]) {
		return d.removeFlow(chain[drop], p)
	}
	return d.removeBlock(chain[drop], p)
}

func (d *document) removeBlock(parent *yaml.Node, p pair) ([]byte, error) {
	if p.key.Column-1 != indentAt(d.src, d.lineStart(p.key.Line)) {
		return nil, fmt.Errorf("line %d: %s does not start its line; change it by hand", p.key.Line, p.key.Value)
	}
	end, err := d.colonAfter(p.key, false)
	if err != nil {
		return nil, err
	}
	if !isEmptyValue(p.value) {
		if end, err = d.nodeEnd(p.value, false, parent.Column-1); err != nil {
			return nil, err
		}
	}
	from := d.lineStart(p.key.Line)
	to := d.lineEnd(end)
	if to < len(d.src) {
		to += breakSize(d.src, to)
	}
	return splice(d.src, from, to, ""), nil
}

func (d *document) removeFlow(parent *yaml.Node, p pair) ([]byte, error) {
	from := d.offset(p.key.Line, p.key.Column)
	end, err := d.colonAfter(p.key, true)
	if err != nil {
		return nil, err
	}
	if !isEmptyValue(p.value) {
		if end, err = d.nodeEnd(p.value, true, -1); err != nil {
			return nil, err
		}
	}
	first := parent.Content[0] == p.key
	if first {
		for end < len(d.src) && (d.src[end] == ' ' || d.src[end] == ',') {
			end++
		}
		return splice(d.src, from, end, ""), nil
	}
	for from > 0 && (d.src[from-1] == ' ' || d.src[from-1] == ',') {
		from--
	}
	return splice(d.src, from, end, ""), nil
}
