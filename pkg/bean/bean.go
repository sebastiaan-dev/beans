package bean

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/adrg/frontmatter"
	"gopkg.in/yaml.v3"
)

// tagPattern matches valid tags: lowercase letters, numbers, and hyphens.
// Must start with a letter, can contain hyphens but not consecutively or at the end.
var tagPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// ValidateTag checks if a tag is valid (lowercase, URL-safe, single word).
func ValidateTag(tag string) error {
	if tag == "" {
		return fmt.Errorf("tag cannot be empty")
	}
	if !tagPattern.MatchString(tag) {
		return fmt.Errorf("invalid tag %q: must be lowercase, start with a letter, and contain only letters, numbers, and hyphens", tag)
	}
	return nil
}

// NormalizeTag converts a tag to its canonical form (lowercase).
func NormalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

// HasTag returns true if the bean has the specified tag.
func (b *Bean) HasTag(tag string) bool {
	normalized := NormalizeTag(tag)
	for _, t := range b.Tags {
		if t == normalized {
			return true
		}
	}
	return false
}

// AddTag adds a tag to the bean if it doesn't already exist.
// Returns an error if the tag is invalid.
func (b *Bean) AddTag(tag string) error {
	normalized := NormalizeTag(tag)
	if err := ValidateTag(normalized); err != nil {
		return err
	}
	if !b.HasTag(normalized) {
		b.Tags = append(b.Tags, normalized)
	}
	return nil
}

// RemoveTag removes a tag from the bean.
func (b *Bean) RemoveTag(tag string) {
	normalized := NormalizeTag(tag)
	result := make([]string, 0, len(b.Tags))
	for _, t := range b.Tags {
		if t != normalized {
			result = append(result, t)
		}
	}
	b.Tags = result
}

// HasParent returns true if the bean has a parent.
func (b *Bean) HasParent() bool {
	return b.Parent != ""
}

// IsBlocking returns true if this bean is blocking the given bean ID.
func (b *Bean) IsBlocking(id string) bool {
	for _, target := range b.Blocking {
		if target == id {
			return true
		}
	}
	return false
}

// AddBlocking adds a bean ID to the blocking list if not already present.
func (b *Bean) AddBlocking(id string) {
	if !b.IsBlocking(id) {
		b.Blocking = append(b.Blocking, id)
	}
}

// RemoveBlocking removes a bean ID from the blocking list.
func (b *Bean) RemoveBlocking(id string) {
	result := make([]string, 0, len(b.Blocking))
	for _, target := range b.Blocking {
		if target != id {
			result = append(result, target)
		}
	}
	b.Blocking = result
}

// IsBlockedBy returns true if this bean is blocked by the given bean ID.
func (b *Bean) IsBlockedBy(id string) bool {
	for _, blocker := range b.BlockedBy {
		if blocker == id {
			return true
		}
	}
	return false
}

// AddBlockedBy adds a bean ID to the blocked-by list if not already present.
func (b *Bean) AddBlockedBy(id string) {
	if !b.IsBlockedBy(id) {
		b.BlockedBy = append(b.BlockedBy, id)
	}
}

// RemoveBlockedBy removes a bean ID from the blocked-by list.
func (b *Bean) RemoveBlockedBy(id string) {
	result := make([]string, 0, len(b.BlockedBy))
	for _, blocker := range b.BlockedBy {
		if blocker != id {
			result = append(result, blocker)
		}
	}
	b.BlockedBy = result
}

// Bean represents an issue stored as a markdown file with front matter.
type Bean struct {
	// ID is the unique NanoID identifier (from filename).
	ID string `yaml:"-" json:"id"`
	// Slug is the optional human-readable part of the filename.
	Slug string `yaml:"-" json:"slug,omitempty"`
	// Path is the relative path from .beans/ root (e.g., "epic-auth/abc123-login.md").
	Path string `yaml:"-" json:"path"`

	// Front matter fields
	Title     string     `yaml:"title" json:"title"`
	Status    string     `yaml:"status" json:"status"`
	Type      string     `yaml:"type,omitempty" json:"type,omitempty"`
	Priority  string     `yaml:"priority,omitempty" json:"priority,omitempty"`
	Tags      []string   `yaml:"tags,omitempty" json:"tags,omitempty"`
	CreatedAt *time.Time `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	UpdatedAt *time.Time `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`

	// Order is a fractional index string for manual sorting.
	Order string `yaml:"order,omitempty" json:"order,omitempty"`

	// Body is the markdown content after the front matter.
	Body string `yaml:"-" json:"body,omitempty"`

	// Parent is the optional parent bean ID (milestone, epic, or feature).
	Parent string `yaml:"parent,omitempty" json:"parent,omitempty"`

	// Blocking is a list of bean IDs that this bean is blocking.
	Blocking []string `yaml:"blocking,omitempty" json:"blocking,omitempty"`

	// BlockedBy is a list of bean IDs that are blocking this bean.
	BlockedBy []string `yaml:"blocked_by,omitempty" json:"blocked_by,omitempty"`

	// extraFrontMatter holds front matter keys that beans does not manage
	// (e.g. metadata added by external tools). They are preserved on rewrite.
	extraFrontMatter []frontMatterField
}

// Default values applied to beans that don't specify them.
const (
	DefaultType     = "task"
	DefaultPriority = "normal"
)

// ApplyDefaults fills in default values for unset fields (type, priority) and
// replaces nil slices with empty ones. Beans are normalized this way whenever
// they are loaded or written, so all readers and writers agree on a bean's
// content and therefore on its etag.
func (b *Bean) ApplyDefaults() {
	if b.Type == "" {
		b.Type = DefaultType
	}
	if b.Priority == "" {
		b.Priority = DefaultPriority
	}
	if b.Tags == nil {
		b.Tags = []string{}
	}
	if b.Blocking == nil {
		b.Blocking = []string{}
	}
}

// frontMatter is the subset of Bean that gets serialized to YAML front matter.
type frontMatter struct {
	Title     string     `yaml:"title"`
	Status    string     `yaml:"status"`
	Type      string     `yaml:"type,omitempty"`
	Priority  string     `yaml:"priority,omitempty"`
	Tags      []string   `yaml:"tags,omitempty"`
	CreatedAt *time.Time `yaml:"created_at,omitempty"`
	UpdatedAt *time.Time `yaml:"updated_at,omitempty"`
	Order     string     `yaml:"order,omitempty"`
	Parent    string     `yaml:"parent,omitempty"`
	Blocking  []string   `yaml:"blocking,omitempty"`
	BlockedBy []string   `yaml:"blocked_by,omitempty"`
}

// knownFrontMatterKeys lists the front matter keys managed by beans.
// Any other key is preserved verbatim as extra front matter.
var knownFrontMatterKeys = map[string]bool{
	"title":      true,
	"status":     true,
	"type":       true,
	"priority":   true,
	"tags":       true,
	"created_at": true,
	"updated_at": true,
	"order":      true,
	"parent":     true,
	"blocking":   true,
	"blocked_by": true,
}

// frontMatterField is a single key/value pair of front matter that beans does
// not manage itself. The YAML nodes are kept as-is so that values (including
// nested structures, comments and styles) round-trip unchanged.
type frontMatterField struct {
	Key   *yaml.Node
	Value *yaml.Node
}

// parsedFrontMatter decodes the known fields and captures all other keys.
type parsedFrontMatter struct {
	known frontMatter
	extra []frontMatterField
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (p *parsedFrontMatter) UnmarshalYAML(node *yaml.Node) error {
	if err := node.Decode(&p.known); err != nil {
		return err
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Kind == yaml.ScalarNode && knownFrontMatterKeys[key.Value] {
			continue
		}
		p.extra = append(p.extra, frontMatterField{Key: key, Value: value})
	}
	return nil
}

// yamlFrontMatter parses YAML front matter with yaml.v3, the same library used
// for rendering, so parse and render agree on syntax.
var yamlFrontMatter = frontmatter.NewFormat("---", "---", yaml.Unmarshal)

// Parse reads a bean from a reader (markdown with YAML front matter).
// Front matter keys not managed by beans are retained and written back by Render.
func Parse(r io.Reader) (*Bean, error) {
	var pfm parsedFrontMatter
	body, err := frontmatter.Parse(r, &pfm, yamlFrontMatter)
	if err != nil {
		return nil, fmt.Errorf("parsing front matter: %w", err)
	}
	fm := pfm.known

	// Trim trailing newline from body (POSIX files end with newline, but it's not part of content)
	bodyStr := strings.TrimSuffix(string(body), "\n")

	return &Bean{
		Title:            fm.Title,
		Status:           fm.Status,
		Type:             fm.Type,
		Priority:         fm.Priority,
		Tags:             fm.Tags,
		CreatedAt:        fm.CreatedAt,
		UpdatedAt:        fm.UpdatedAt,
		Order:            fm.Order,
		Body:             bodyStr,
		Parent:           fm.Parent,
		Blocking:         fm.Blocking,
		BlockedBy:        fm.BlockedBy,
		extraFrontMatter: pfm.extra,
	}, nil
}

// Render serializes the bean back to markdown with YAML front matter.
func (b *Bean) Render() ([]byte, error) {
	fm := frontMatter{
		Title:     b.Title,
		Status:    b.Status,
		Type:      b.Type,
		Priority:  b.Priority,
		Tags:      b.Tags,
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.UpdatedAt,
		Order:     b.Order,
		Parent:    b.Parent,
		Blocking:  b.Blocking,
		BlockedBy: b.BlockedBy,
	}

	var node yaml.Node
	if err := node.Encode(&fm); err != nil {
		return nil, fmt.Errorf("marshaling front matter: %w", err)
	}
	// Append front matter keys that beans does not manage, preserving their order
	for _, f := range b.extraFrontMatter {
		node.Content = append(node.Content, f.Key, f.Value)
	}

	var fmBuf bytes.Buffer
	enc := yaml.NewEncoder(&fmBuf)
	enc.SetIndent(4)
	if err := enc.Encode(&node); err != nil {
		return nil, fmt.Errorf("marshaling front matter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("marshaling front matter: %w", err)
	}
	fmBytes := fmBuf.Bytes()

	var buf bytes.Buffer
	buf.WriteString("---\n")
	if b.ID != "" {
		buf.WriteString("# ")
		buf.WriteString(b.ID)
		buf.WriteString("\n")
	}
	buf.Write(fmBytes)
	buf.WriteString("---\n")
	if b.Body != "" {
		// Only add newline separator if body doesn't already start with one
		if !strings.HasPrefix(b.Body, "\n") {
			buf.WriteString("\n")
		}
		buf.WriteString(b.Body)
		// Ensure trailing newline if body doesn't end with one
		if !strings.HasSuffix(b.Body, "\n") {
			buf.WriteString("\n")
		}
	} else {
		// Even without body, add trailing newline for POSIX compliance
		buf.WriteString("\n")
	}

	return buf.Bytes(), nil
}

// ETag returns a hash of the bean's rendered content for optimistic concurrency control.
// Uses FNV-1a 64-bit hash, producing a 16-character hex string.
// Returns "0000000000000000" if rendering fails (should never happen for valid beans).
func (b *Bean) ETag() string {
	content, err := b.Render()
	if err != nil {
		// Return a sentinel value that will never match a real ETag,
		// ensuring validation will fail rather than silently passing.
		return "0000000000000000"
	}
	h := fnv.New64a()
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// MarshalJSON implements json.Marshaler to include computed etag field.
func (b *Bean) MarshalJSON() ([]byte, error) {
	type BeanAlias Bean // Avoid infinite recursion
	return json.Marshal(&struct {
		*BeanAlias
		ETag string `json:"etag"`
	}{
		BeanAlias: (*BeanAlias)(b),
		ETag:      b.ETag(),
	})
}
