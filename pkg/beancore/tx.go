package beancore

import "github.com/hmans/beans/pkg/bean"

// Tx is a read-only view of the store for the mutation and validation
// callbacks of Mutate and WithValidation. It is used while Core holds its
// write lock, so checks made through it see exactly the state the write is
// applied to. A Tx must not be used after the callback returns, and beans it
// returns are shared snapshots that must not be modified.
//
// The bean being mutated or created is visible through the Tx in its pending
// (modified) state, so checks also cover links added earlier in the same
// change.
type Tx struct {
	c       *Core
	pending *bean.Bean // bean being written, shadows its stored version
}

// lookup returns the bean with the exact ID, preferring the pending bean.
func (tx *Tx) lookup(id string) (*bean.Bean, bool) {
	if tx.pending != nil && tx.pending.ID == id {
		return tx.pending, true
	}
	b, ok := tx.c.beans[id]
	return b, ok
}

// Get returns the bean with the given ID (short IDs are supported).
func (tx *Tx) Get(id string) (*bean.Bean, bool) {
	if b, ok := tx.lookup(id); ok {
		return b, true
	}
	if full := tx.c.normalizeID(id); full != id {
		return tx.lookup(full)
	}
	return nil, false
}

// NormalizeID resolves a potentially short ID to its full form, like
// Core.NormalizeID.
func (tx *Tx) NormalizeID(id string) (string, bool) {
	if _, ok := tx.lookup(id); ok {
		return id, true
	}
	if full := tx.c.normalizeID(id); full != id {
		if _, ok := tx.lookup(full); ok {
			return full, true
		}
	}
	return id, false
}

// ValidateParent checks if parentID is a valid parent for b: the parent must
// exist and its type must be allowed for b's type. Returns nil if valid.
func (tx *Tx) ValidateParent(b *bean.Bean, parentID string) error {
	if parentID == "" {
		return nil
	}

	validTypes := ValidParentTypes(b.Type)
	if validTypes == nil {
		return errNoParentAllowed(b.Type)
	}

	parent, ok := tx.Get(parentID)
	if !ok {
		return errParentNotFound(parentID)
	}

	for _, t := range validTypes {
		if parent.Type == t {
			return nil
		}
	}
	return errInvalidParentType(b.Type, validTypes, parent.Type)
}

// DetectCycle checks if adding a link from fromID to toID would create a
// cycle. Checks for blocking, blocked_by, and parent link types. Returns the
// cycle path if a cycle would be created, nil otherwise.
func (tx *Tx) DetectCycle(fromID, linkType, toID string) []string {
	// Only check hierarchical link types
	if linkType != "blocking" && linkType != "blocked_by" && linkType != "parent" {
		return nil
	}

	// Adding edge: fromID -> toID. Check if there's already a path from toID
	// back to fromID.
	visited := make(map[string]bool)
	path := []string{fromID, toID}
	return tx.findPathToTarget(toID, fromID, linkType, visited, path)
}

// findPathToTarget uses DFS to find if there's a path from current to target.
// Returns the path if found, nil otherwise.
func (tx *Tx) findPathToTarget(current, target, linkType string, visited map[string]bool, path []string) []string {
	if current == target {
		return path
	}

	if visited[current] {
		return nil
	}
	visited[current] = true

	b, ok := tx.lookup(current)
	if !ok {
		return nil
	}

	// Get targets based on link type
	var targets []string
	switch linkType {
	case "parent":
		if b.Parent != "" {
			targets = []string{b.Parent}
		}
	case "blocking":
		targets = b.Blocking
	case "blocked_by":
		targets = b.BlockedBy
	}

	for _, t := range targets {
		newPath := append(path, t)
		if result := tx.findPathToTarget(t, target, linkType, visited, newPath); result != nil {
			return result
		}
	}

	return nil
}
