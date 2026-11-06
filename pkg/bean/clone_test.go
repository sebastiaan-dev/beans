package bean

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestClone(t *testing.T) {
	input := `---
title: Original
status: todo
type: task
tags:
    - a
created_at: 2025-01-01T00:00:00Z
updated_at: 2025-01-02T00:00:00Z
parent: p1
blocking:
    - b1
blocked_by:
    - c1
custom:
    nested:
        - x
---

Body text
`
	orig, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	orig.ID = "id1"
	origETag := orig.ETag()
	origRender, _ := orig.Render()

	c := orig.Clone()
	if !reflect.DeepEqual(c, orig) {
		t.Fatalf("clone differs:\n%+v\n%+v", c, orig)
	}
	if c.ETag() != origETag {
		t.Errorf("clone etag = %s, want %s", c.ETag(), origETag)
	}

	// Modify everything reachable from the clone.
	c.Title = "Changed"
	c.Tags[0] = "changed"
	c.Tags = append(c.Tags, "more")
	c.Blocking[0] = "changed"
	c.BlockedBy[0] = "changed"
	*c.CreatedAt = c.CreatedAt.Add(time.Hour)
	*c.UpdatedAt = c.UpdatedAt.Add(time.Hour)
	c.extraFrontMatter[0].Value.Content[1].Content[0].Value = "changed"

	if got, _ := orig.Render(); string(got) != string(origRender) {
		t.Errorf("modifying the clone changed the original:\n%s", got)
	}
	if orig.ETag() != origETag {
		t.Error("original etag changed")
	}
}

func TestCloneKeepsNilAndEmptySlices(t *testing.T) {
	b := &Bean{ID: "x", Tags: []string{}, Blocking: nil}
	c := b.Clone()
	if c.Tags == nil || len(c.Tags) != 0 {
		t.Errorf("Tags = %#v, want empty non-nil", c.Tags)
	}
	if c.Blocking != nil || c.BlockedBy != nil || c.CreatedAt != nil {
		t.Errorf("nil fields should stay nil: %+v", c)
	}
	if (*Bean)(nil).Clone() != nil {
		t.Error("nil.Clone() should be nil")
	}
}
