package ticket

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

var (
	beforeShot = Attachment{ID: "9f2c", Role: AttachmentBefore, Link: "https://folio.example/api/images/9f2c"}
	afterShot  = Attachment{ID: "0a.B_-", Role: AttachmentAfter, Link: "https://folio.example:8443/i/0a?w=1&h=2,3&t=[x]{y}'\"%20!#a:b"}
)

func mustSet(t *testing.T, tk *Ticket, as ...Attachment) {
	t.Helper()
	for _, a := range as {
		if err := SetAttachment(tk, a); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAttachmentRejectsUnknownRoleOrLink(t *testing.T) {
	for _, a := range []Attachment{
		{"id", "", "https://h/x"},
		{"id", "during", "https://h/x"},
		{"id", "Before", "https://h/x"},
		{"id", AttachmentAfter, ""},
		{"id", AttachmentAfter, "http://h/x"},
		{"id", AttachmentAfter, "file:///etc/passwd"},
		{"id", AttachmentAfter, "javascript:alert(1)"},
		{"id", AttachmentAfter, "h/x"},
		{"id", AttachmentAfter, "https:///x"},
		{"id", AttachmentAfter, "https://:443/x"},
		{"id", AttachmentAfter, "https:h/x"},
		{"id", AttachmentAfter, "https://h/a b"},
		{"id", AttachmentAfter, "https://h/a\nparent: x"},
		{"id", AttachmentAfter, "https://h/a\u0085b"},
		{"id", AttachmentAfter, "https://h/a\xff"},
		// Plain, a trailing colon reads back as a mapping key.
		{"id", AttachmentAfter, "https://h/a:"},
		{"", AttachmentAfter, "https://h/x"},
		{"-id", AttachmentAfter, "https://h/x"},
		{"a b", AttachmentAfter, "https://h/x"},
		{"a:b", AttachmentAfter, "https://h/x"},
		{"a\n", AttachmentAfter, "https://h/x"},
	} {
		tk := &Ticket{}
		mustSet(t, tk, beforeShot)
		if err := SetAttachment(tk, a); err == nil {
			t.Errorf("SetAttachment(%+v) was accepted", a)
		}
		if got := Attachments(tk); !reflect.DeepEqual(got, []Attachment{beforeShot}) {
			t.Errorf("a refused %+v left %+v", a, got)
		}
	}
	if err := RemoveAttachment(&Ticket{}, "during"); err == nil {
		t.Error("RemoveAttachment accepted an unknown role")
	}
	// The stored keys and the served one are not extra fields.
	for _, key := range []string{"attachments", "attachment-before", "attachment-after"} {
		if err := ValidateExtraKey(key); err == nil {
			t.Errorf("ValidateExtraKey(%q) was accepted", key)
		}
	}
}

func TestAttachmentAddReplaceRemove(t *testing.T) {
	tk := &Ticket{}
	replacement := Attachment{ID: "new", Role: AttachmentBefore, Link: "https://folio.example/new"}
	mustSet(t, tk, afterShot, beforeShot)
	if got := Attachments(tk); !reflect.DeepEqual(got, []Attachment{beforeShot, afterShot}) {
		t.Errorf("attachments = %+v, want before then after", got)
	}
	mustSet(t, tk, replacement)
	if got := Attachments(tk); !reflect.DeepEqual(got, []Attachment{replacement, afterShot}) {
		t.Errorf("after a second before = %+v", got)
	}
	for range 2 {
		if err := RemoveAttachment(tk, AttachmentBefore); err != nil {
			t.Fatal(err)
		}
		if got := Attachments(tk); !reflect.DeepEqual(got, []Attachment{afterShot}) {
			t.Errorf("after the remove = %+v", got)
		}
	}
}

// The trail names attachments rather than the extra fields they are stored as.
func TestAttachmentChangeIsNamedInMutationLog(t *testing.T) {
	prior, next := &Ticket{}, &Ticket{}
	mustSet(t, next, beforeShot)
	if got := changedFields(prior, next); !slices.Equal(got, []string{"attachments"}) {
		t.Errorf("changed fields = %v", got)
	}
}

// A hand-edited value that is not an attachment is not served as one and is not
// lost: it stays an extra field, and `attachments` set as an extra before the
// name was taken stays in the file without being served under it.
func TestAttachmentMalformedOrLegacyValueIsKept(t *testing.T) {
	tk, err := Parse(strings.NewReader("---\nid: a-1\nstatus: open\ntype: feature\npriority: 2\nattachment-after: x javascript:alert(1)\nattachments: legacy\nenv: prod\n---\n# T\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := Attachments(tk); len(got) != 0 {
		t.Errorf("attachments = %+v, want none", got)
	}
	want := map[string]string{"attachment-after": "x javascript:alert(1)", "env": "prod"}
	if got := ExtraFields(tk); !reflect.DeepEqual(got, want) {
		t.Errorf("extra fields = %v, want %v", got, want)
	}
	data, err := Serialize(tk)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"attachment-after: x javascript:alert(1)\n", "attachments: legacy\n"} {
		if !strings.Contains(string(data), line) {
			t.Errorf("serialized ticket lost %q:\n%s", line, data)
		}
	}
	mustSet(t, tk, afterShot)
	if got := ExtraFields(tk); !reflect.DeepEqual(got, map[string]string{"env": "prod"}) {
		t.Errorf("extra fields beside attachments = %v", got)
	}
}

// unawareWrite rewrites a ticket file the way a tk that does not know
// attachments does: every frontmatter key it has no field for is kept as Go's
// print of the decoded value and written back with scalar.
func unawareWrite(t *testing.T, path, title string, scalar func(string) string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	front, body, ok := strings.Cut(strings.TrimPrefix(string(data), "---\n"), "\n---\n")
	if !ok {
		t.Fatalf("no frontmatter:\n%s", data)
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(front), &raw); err != nil {
		t.Fatal(err)
	}
	known := []string{"id", "status", "deps", "links", "created", "updated", "type", "priority"}
	var out strings.Builder
	out.WriteString("---\n")
	for _, line := range strings.Split(front, "\n") {
		if key, _, _ := strings.Cut(line, ":"); slices.Contains(known, key) {
			out.WriteString(line + "\n")
		}
	}
	for _, key := range slices.Sorted(maps.Keys(raw)) {
		if !slices.Contains(known, key) {
			out.WriteString(key + ": " + scalar(fmt.Sprintf("%v", raw[key])) + "\n")
		}
	}
	_, rest, _ := strings.Cut(body, "\n")
	out.WriteString("---\n# " + title + "\n" + rest)
	if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAttachmentsSurviveUnawareWrite(t *testing.T) {
	for name, scalar := range map[string]func(string) string{
		"unquoted writer, before v8.6.0": func(v string) string { return v },
		"quoting writer":                 yamlScalar,
	} {
		t.Run(name, func(t *testing.T) {
			store := NewFileStore(t.TempDir())
			tk := &Ticket{ID: "un-1", Status: StatusOpen, Type: TypeFeature, Created: time.Now(), Title: "Before", Body: "Why.\n"}
			if err := store.Create(tk); err != nil {
				t.Fatal(err)
			}
			if _, err := Mutate(store, "un-1", func(t *Ticket) error {
				if err := SetAttachment(t, beforeShot); err != nil {
					return err
				}
				return SetAttachment(t, afterShot)
			}); err != nil {
				t.Fatal(err)
			}

			// Twice, so what the first write leaves survives the next one too.
			for _, title := range []string{"Once", "Twice"} {
				unawareWrite(t, filepath.Join(store.Dir, "un-1.md"), title, scalar)
				got, err := store.Get("un-1")
				if err != nil {
					t.Fatal(err)
				}
				if got.Title != title || got.Body != "Why.\n" {
					t.Errorf("title %q body %q, want the unaware write's", got.Title, got.Body)
				}
				if as := Attachments(got); !reflect.DeepEqual(as, []Attachment{beforeShot, afterShot}) {
					t.Errorf("attachments after %q = %+v", title, as)
				}
				if extra := ExtraFields(got); len(extra) != 0 {
					t.Errorf("extra fields after %q = %v", title, extra)
				}
			}
		})
	}
}
