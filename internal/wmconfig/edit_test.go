package wmconfig

import (
	"strings"
	"testing"
)

const annotated = `# my lys-wm config

# the look
gaps:
  inner: 4   # tight
  outer: 8

decoration: {rounding: 6, shadow: {enabled: false}}

future_key: keep me
binds:
  - {chord: SUPER + Return, action: exec foot}  # terminal
  # a commented-out bind
  - {chord: SUPER + Q, action: kill}

env:
  FOO: "bar # not a comment"
  URL: http://example.org/a:b
autostart:
  - lys-shell shell

# the end
`

func edit(t *testing.T, src string, e Edit) string {
	t.Helper()
	out, err := ApplyEdit([]byte(src), e)
	if err != nil {
		t.Fatalf("%s: %v", e, err)
	}
	return string(out)
}

func keys(path string) []string { return strings.Split(path, ".") }

func TestAnEditChangesOnlyTheBytesOfItsValue(t *testing.T) {
	for _, tc := range []struct {
		path string
		v    any
		old  string
		new  string
	}{
		{"gaps.inner", 12, "inner: 4   # tight", "inner: 12   # tight"},
		{"gaps.outer", int64(0), "outer: 8", "outer: 0"},
		{"decoration.rounding", 2, "{rounding: 6,", "{rounding: 2,"},
		{"decoration.shadow.enabled", true, "{enabled: false}", "{enabled: true}"},
		{"env.FOO", "baz", `FOO: "bar # not a comment"`, "FOO: baz"},
		{"env.URL", "x", "URL: http://example.org/a:b", "URL: x"},
		{"future_key", "still mine", "future_key: keep me", "future_key: still mine"},
	} {
		got := edit(t, annotated, Edit{Path: keys(tc.path), Value: tc.v})
		want := strings.Replace(annotated, tc.old, tc.new, 1)
		if got != want {
			t.Errorf("%s:\n%s\nwant\n%s", tc.path, got, want)
		}
	}
}

func TestANewKeyJoinsItsMappingAndLeavesTheRest(t *testing.T) {
	got := edit(t, annotated, Edit{Path: keys("gaps.extra"), Value: 1})
	if want := strings.Replace(annotated, "  outer: 8\n", "  outer: 8\n  extra: 1\n", 1); got != want {
		t.Errorf("into a block mapping:\n%s", got)
	}
	got = edit(t, annotated, Edit{Path: keys("decoration.dim_inactive"), Value: true})
	if want := strings.Replace(annotated, "{enabled: false}}", "{enabled: false}, dim_inactive: true}", 1); got != want {
		t.Errorf("into a flow mapping:\n%s", got)
	}
	got = edit(t, annotated, Edit{Path: keys("input.keyboard.layout"), Value: "dk"})
	if want := strings.Replace(annotated, "  - lys-shell shell\n", "  - lys-shell shell\ninput:\n  keyboard:\n    layout: dk\n", 1); got != want {
		t.Errorf("a new nested mapping:\n%s", got)
	}
	got = edit(t, annotated, Edit{Path: keys("decoration.blur.enabled"), Value: true})
	if want := strings.Replace(annotated, "{enabled: false}}", "{enabled: false}, blur: {enabled: true}}", 1); got != want {
		t.Errorf("a new nested mapping in a flow one:\n%s", got)
	}
}

func TestReplacingAListKeepsWhatFollows(t *testing.T) {
	got := edit(t, annotated, Edit{Path: keys("binds"), Value: []any{map[string]any{"chord": "SUPER + X", "action": "kill"}}})
	if !strings.Contains(got, "binds:\n  - action: kill\n    chord: SUPER + X\n\nenv:\n") {
		t.Fatalf("the list did not land in place:\n%s", got)
	}
	if !strings.Contains(got, "# my lys-wm config") || !strings.Contains(got, "# the end") || !strings.Contains(got, "future_key: keep me") {
		t.Fatalf("lost what the edit did not touch:\n%s", got)
	}
}

func TestReplacingABlockMappingWithAScalarAndBack(t *testing.T) {
	got := edit(t, "gaps:\n  inner: 1\n  outer: 2\ntearing: true\n", Edit{Path: keys("gaps"), Value: 3})
	if got != "gaps: 3\ntearing: true\n" {
		t.Fatalf("block to scalar: %q", got)
	}
	got = edit(t, "gaps: 3\ntearing: true\n", Edit{Path: keys("gaps"), Value: map[string]any{"inner": 1}})
	if got != "gaps:\n  inner: 1\ntearing: true\n" {
		t.Fatalf("scalar to block: %q", got)
	}
}

func TestAKeyWithNoValueTakesOne(t *testing.T) {
	if got := edit(t, "gaps:\ntearing: true\n", Edit{Path: keys("gaps.inner"), Value: 1}); got != "gaps:\n  inner: 1\ntearing: true\n" {
		t.Fatalf("%q", got)
	}
	if got := edit(t, "tearing:\n", Edit{Path: keys("tearing"), Value: true}); got != "tearing: true\n" {
		t.Fatalf("%q", got)
	}
}

func TestAMultiLineStringIsReplacedWhole(t *testing.T) {
	src := "motd: |\n  one\n  two\n\ntearing: true\n"
	if got := edit(t, src, Edit{Path: keys("motd"), Value: "x"}); got != "motd: x\n\ntearing: true\n" {
		t.Fatalf("%q", got)
	}
	src = "motd: one\n  two\ntearing: true\n"
	if got := edit(t, src, Edit{Path: keys("motd"), Value: "x"}); got != "motd: x\ntearing: true\n" {
		t.Fatalf("a folded plain scalar: %q", got)
	}
}

func TestAnEmptyOrCommentOnlyFileTakesTheKeyAtItsEnd(t *testing.T) {
	for src, want := range map[string]string{
		"":                   "gaps:\n  inner: 1\n",
		"# only a comment\n": "# only a comment\ngaps:\n  inner: 1\n",
		"# no newline":       "# no newline\ngaps:\n  inner: 1\n",
	} {
		if got := edit(t, src, Edit{Path: keys("gaps.inner"), Value: 1}); got != want {
			t.Errorf("%q: %q", src, got)
		}
	}
}

func TestAFileWithoutATrailingNewlineStaysValid(t *testing.T) {
	if got := edit(t, "tearing: true", Edit{Path: keys("gaps"), Value: 1}); got != "tearing: true\ngaps: 1" {
		t.Fatalf("%q", got)
	}
}

func TestNonASCIIBeforeTheValueDoesNotMisplaceIt(t *testing.T) {
	src := "env: {GRÜSSE: \"æøå\", B: 1}\n"
	if got := edit(t, src, Edit{Path: keys("env.B"), Value: 2}); got != "env: {GRÜSSE: \"æøå\", B: 2}\n" {
		t.Fatalf("%q", got)
	}
}

func TestWindowsLineEndingsSurviveAnEdit(t *testing.T) {
	src := "gaps:\r\n  inner: 1\r\n  outer: 2\r\ntearing: true\r\n"
	if got := edit(t, src, Edit{Path: keys("gaps.outer"), Value: 3}); got != "gaps:\r\n  inner: 1\r\n  outer: 3\r\ntearing: true\r\n" {
		t.Fatalf("set: %q", got)
	}
	if got := edit(t, src, Edit{Path: keys("gaps.outer"), Unset: true}); got != "gaps:\r\n  inner: 1\r\ntearing: true\r\n" {
		t.Fatalf("unset: %q", got)
	}
}

func TestUnsetRemovesTheKeyAndAMappingItEmptied(t *testing.T) {
	got := edit(t, annotated, Edit{Path: keys("gaps.outer"), Unset: true})
	if want := strings.Replace(annotated, "  outer: 8\n", "", 1); got != want {
		t.Errorf("one of two:\n%s", got)
	}
	got = edit(t, "gaps:\n  inner: 1\ntearing: true\n", Edit{Path: keys("gaps.inner"), Unset: true})
	if got != "tearing: true\n" {
		t.Errorf("the last of a mapping: %q", got)
	}
	got = edit(t, "a: {b: 1, c: 2}\n", Edit{Path: keys("a.b"), Unset: true})
	if got != "a: {c: 2}\n" {
		t.Errorf("first in flow: %q", got)
	}
	got = edit(t, "a: {b: 1, c: 2}\n", Edit{Path: keys("a.c"), Unset: true})
	if got != "a: {b: 1}\n" {
		t.Errorf("last in flow: %q", got)
	}
	got = edit(t, "a: {b: 1}\nz: 1\n", Edit{Path: keys("a.b"), Unset: true})
	if got != "z: 1\n" {
		t.Errorf("only in flow: %q", got)
	}
	got = edit(t, annotated, Edit{Path: keys("binds"), Unset: true})
	if strings.Contains(got, "chord") || strings.Contains(got, "a commented-out bind") || !strings.Contains(got, "env:\n") {
		t.Errorf("a whole list:\n%s", got)
	}
}

func TestUnsettingWhatIsNotThereChangesNothing(t *testing.T) {
	for _, path := range []string{"nope", "gaps.nope", "tearing.x", "nope.nope"} {
		src := "gaps:\n  inner: 1 # c\ntearing: true\n"
		if got := edit(t, src, Edit{Path: keys(path), Unset: true}); got != src {
			t.Errorf("%s: %q", path, got)
		}
	}
	if got := edit(t, "", Edit{Path: keys("a"), Unset: true}); got != "" {
		t.Errorf("an empty file: %q", got)
	}
}

func TestWhatCannotBeEditedSafelyIsRefusedAndNamesTheLine(t *testing.T) {
	for name, tc := range map[string]struct {
		src  string
		e    Edit
		want string
	}{
		"not yaml":        {"gaps: [\n", Edit{Path: keys("gaps"), Value: 1}, "does not parse"},
		"not a mapping":   {"- a\n- b\n", Edit{Path: keys("gaps"), Value: 1}, "does not parse"},
		"through scalar":  {"gaps: 3\n", Edit{Path: keys("gaps.inner"), Value: 1}, "line 1: gaps is not a mapping"},
		"through a list":  {"gaps:\n  - 3\n", Edit{Path: keys("gaps.inner"), Value: 1}, "line 1: gaps is not a mapping"},
		"an alias":        {"x: &a {b: 1}\ngaps: *a\n", Edit{Path: keys("gaps"), Value: 1}, "line 2: gaps is an alias"},
		"through alias":   {"x: &a {b: 1}\ngaps: *a\n", Edit{Path: keys("gaps.b"), Value: 2}, "line 2"},
		"an anchor":       {"x: &a {b: 1}\ny: *a\n", Edit{Path: keys("x.b"), Value: 2}, "line 1: x is an anchor"},
		"a merge":         {"x: &a {b: 1}\ny:\n  <<: *a\n  c: 1\n", Edit{Path: keys("y.b"), Value: 2}, "line 3 merges"},
		"no key":          {"a: 1\n", Edit{}, "needs a key"},
		"unencodable":     {"a: 1\n", Edit{Path: keys("a"), Value: make(chan int)}, "encode"},
		"an anchored one": {"a: &x 1\nb: *x\n", Edit{Path: keys("a"), Value: 2}, "line 1: a is an anchor"},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil && name != "unencodable" {
					t.Fatalf("panicked: %v", r)
				}
			}()
			out, err := ApplyEdit([]byte(tc.src), tc.e)
			if err == nil {
				t.Fatalf("accepted: %q", out)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v, want it to say %q", err, tc.want)
			}
		})
	}
}

func FuzzAnEditEitherDoesWhatItSaysOrRefuses(f *testing.F) {
	for _, seed := range []string{annotated, "", "a: 1", "a:\n  b: {c: [1, 2]}\n", "a: 'x''y' # c\n", "a: |\n  b\nc: 1\n", "? a\n: b\n"} {
		f.Add(seed, "gaps.inner", int64(3), false)
		f.Add(seed, "a", int64(1), true)
	}
	f.Fuzz(func(t *testing.T, src, path string, v int64, remove bool) {
		if path == "" {
			return
		}
		e := Edit{Path: strings.Split(path, "."), Value: v, Unset: remove}
		out, err := ApplyEdit([]byte(src), e)
		if err != nil {
			return
		}
		tree, err := decodeTree(out)
		if err != nil {
			t.Fatalf("accepted a result that does not parse: %q", out)
		}
		if remove {
			return
		}
		var node any = tree
		for _, key := range e.Path {
			m, ok := node.(map[string]any)
			if !ok {
				t.Fatalf("%q has no %s", out, path)
			}
			node = m[key]
		}
		if node != any(int(v)) {
			t.Fatalf("%q: %s is %v", out, path, node)
		}
	})
}
