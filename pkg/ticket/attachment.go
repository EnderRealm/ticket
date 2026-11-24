package ticket

import (
	"fmt"
	"maps"
	"net/url"
	"strings"
)

// AttachmentRole says which picture of a ticket an attachment is.
type AttachmentRole string

const (
	// AttachmentBefore is the picture the ticket started from.
	AttachmentBefore AttachmentRole = "before"
	// AttachmentAfter is the picture the run captured.
	AttachmentAfter AttachmentRole = "after"
)

// attachmentRoles is every role, in the order attachments are listed.
var attachmentRoles = []AttachmentRole{AttachmentBefore, AttachmentAfter}

// Attachment references a picture hosted elsewhere; the ticket holds no bytes.
// A ticket has at most one per role.
//
// It is stored as one extra, `attachment-<role>: <id> <link>`, and not as a
// block: a tk released before attachments existed keeps an unknown frontmatter
// key as the text of a scalar, so a write by one carries this back unchanged
// where a list or a mapping would come back as Go's print of it. Before v8.6.0
// that text was also written back unquoted, so the value has to be plain YAML.
type Attachment struct {
	ID   string         `json:"id"`
	Role AttachmentRole `json:"role"`
	Link string         `json:"link"`
}

// attachmentsKey is the JSON key attachments are served under.
const attachmentsKey = "attachments"

// attachmentKeys are refused as extra field names. They are kept apart from
// reservedKeys because Parse drops those from Extra: the stored keys live
// there, and an `attachments` extra set before the name was taken stays in its
// file.
var attachmentKeys = map[string]bool{
	attachmentsKey:               true,
	AttachmentBefore.storedKey(): true,
	AttachmentAfter.storedKey():  true,
}

func (r AttachmentRole) storedKey() string {
	return "attachment-" + string(r)
}

func (a Attachment) storedValue() string {
	return a.ID + " " + a.Link
}

// ValidateAttachmentRole returns an error if r is not a recognized role.
func ValidateAttachmentRole(r AttachmentRole) error {
	if r == AttachmentBefore || r == AttachmentAfter {
		return nil
	}
	return fmt.Errorf("invalid attachment role %q: must be one of before, after", r)
}

// Validate checks that a can be stored and read back as the same attachment.
func (a Attachment) Validate() error {
	if err := ValidateAttachmentRole(a.Role); err != nil {
		return err
	}
	if a.ID == "" {
		return fmt.Errorf("attachment id must not be empty")
	}
	for i, c := range a.ID {
		alnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !alnum && (i == 0 || !strings.ContainsRune("._-", c)) {
			return fmt.Errorf("invalid attachment id %q: must be letters, digits, '.', '_' and '-', starting with a letter or digit", a.ID)
		}
	}
	for _, c := range a.Link {
		if c <= ' ' || c > '~' {
			return fmt.Errorf("invalid attachment link %q: contains %q; percent-encode anything outside printable ASCII", a.Link, c)
		}
	}
	u, err := url.Parse(a.Link)
	if err != nil {
		return fmt.Errorf("invalid attachment link: %w", err)
	}
	if u.Scheme != "https" || u.Hostname() == "" {
		return fmt.Errorf("invalid attachment link %q: must be an https URL with a host", a.Link)
	}
	if v := a.storedValue(); yamlScalar(v) != v {
		return fmt.Errorf("invalid attachment link %q: does not read back as plain YAML; percent-encode its punctuation", a.Link)
	}
	return nil
}

// Attachments returns t's attachments, before then after. A stored value that
// is not a valid attachment, which only a hand edit leaves, is none.
func Attachments(t *Ticket) []Attachment {
	var out []Attachment
	for _, role := range attachmentRoles {
		id, link, _ := strings.Cut(t.Extra[role.storedKey()], " ")
		a := Attachment{ID: id, Role: role, Link: link}
		if a.Validate() == nil {
			out = append(out, a)
		}
	}
	return out
}

// SetAttachment adds a to t, replacing the attachment with the same role.
func SetAttachment(t *Ticket, a Attachment) error {
	if err := a.Validate(); err != nil {
		return err
	}
	if t.Extra == nil {
		t.Extra = map[string]string{}
	}
	t.Extra[a.Role.storedKey()] = a.storedValue()
	return nil
}

// RemoveAttachment removes t's attachment with that role, if it has one.
func RemoveAttachment(t *Ticket, role AttachmentRole) error {
	if err := ValidateAttachmentRole(role); err != nil {
		return err
	}
	delete(t.Extra, role.storedKey())
	return nil
}

// ExtraFields is t's extra fields as JSON flattens them to the top level:
// without the attachments Attachments returns, and without an extra of the
// name they are served under.
func ExtraFields(t *Ticket) map[string]string {
	fields := maps.Clone(t.Extra)
	delete(fields, attachmentsKey)
	for _, a := range Attachments(t) {
		delete(fields, a.Role.storedKey())
	}
	return fields
}
