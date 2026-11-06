package beangraph

import (
	"fmt"

	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beancore"
)

// CoreResolver implements the core bean GraphQL operations (CRUD, relationships,
// filtering). It depends only on beancore.Core and has no UI-specific dependencies
// (agents, worktrees, terminals, etc.).
//
// CLI commands use CoreResolver directly. The serve command's full GraphQL resolver
// embeds CoreResolver and adds UI-specific operations on top.
type CoreResolver struct {
	Core *beancore.Core
}

// ETagMismatchError is returned when an ETag validation fails.
// This allows callers to distinguish concurrency conflicts from other errors.
type ETagMismatchError struct {
	Provided string
	Current  string
}

func (e *ETagMismatchError) Error() string {
	return fmt.Sprintf("etag mismatch: provided %s, current is %s", e.Provided, e.Current)
}

// ETagRequiredError is returned when require_if_match is enabled and no ETag is provided.
type ETagRequiredError struct{}

func (e *ETagRequiredError) Error() string {
	return "if-match etag is required (set require_if_match: false in config to disable)"
}

// validateAndSetParent validates and sets the parent relationship.
func validateAndSetParent(tx *beancore.Tx, b *bean.Bean, parentID string) error {
	if parentID == "" {
		b.Parent = ""
		return nil
	}

	// Normalise short ID to full ID
	normalizedParent, _ := tx.NormalizeID(parentID)

	// Validate parent type hierarchy
	if err := tx.ValidateParent(b, normalizedParent); err != nil {
		return err
	}

	// Check for cycles
	if cycle := tx.DetectCycle(b.ID, "parent", normalizedParent); cycle != nil {
		return fmt.Errorf("setting parent would create cycle: %v", cycle)
	}

	b.Parent = normalizedParent
	return nil
}

// validateAndAddBlocking validates and adds blocking relationships.
func validateAndAddBlocking(tx *beancore.Tx, b *bean.Bean, targetIDs []string) error {
	for _, targetID := range targetIDs {
		// Normalise short ID to full ID
		normalizedTargetID, _ := tx.NormalizeID(targetID)

		// Validate: cannot block itself
		if normalizedTargetID == b.ID {
			return fmt.Errorf("bean cannot block itself")
		}

		// Validate: target must exist
		if _, ok := tx.Get(normalizedTargetID); !ok {
			return fmt.Errorf("blocking target bean not found: %s", targetID)
		}

		// Check for cycles in both directions
		if cycle := tx.DetectCycle(b.ID, "blocking", normalizedTargetID); cycle != nil {
			return fmt.Errorf("adding blocking relationship would create cycle: %v", cycle)
		}
		if cycle := tx.DetectCycle(normalizedTargetID, "blocked_by", b.ID); cycle != nil {
			return fmt.Errorf("adding blocking relationship would create cycle: %v", cycle)
		}

		b.AddBlocking(normalizedTargetID)
	}
	return nil
}

// removeBlockingRelationships removes blocking relationships.
func removeBlockingRelationships(tx *beancore.Tx, b *bean.Bean, targetIDs []string) {
	for _, targetID := range targetIDs {
		normalizedTargetID, _ := tx.NormalizeID(targetID)
		b.RemoveBlocking(normalizedTargetID)
	}
}

// validateAndAddBlockedBy validates and adds blocked-by relationships.
func validateAndAddBlockedBy(tx *beancore.Tx, b *bean.Bean, targetIDs []string) error {
	for _, targetID := range targetIDs {
		// Normalise short ID to full ID
		normalizedTargetID, _ := tx.NormalizeID(targetID)

		// Validate: cannot be blocked by itself
		if normalizedTargetID == b.ID {
			return fmt.Errorf("bean cannot be blocked by itself")
		}

		// Validate: blocker must exist
		if _, ok := tx.Get(normalizedTargetID); !ok {
			return fmt.Errorf("blocker bean not found: %s", targetID)
		}

		// Check for cycles in both directions
		if cycle := tx.DetectCycle(normalizedTargetID, "blocking", b.ID); cycle != nil {
			return fmt.Errorf("adding blocked-by relationship would create cycle: %v", cycle)
		}
		if cycle := tx.DetectCycle(b.ID, "blocked_by", normalizedTargetID); cycle != nil {
			return fmt.Errorf("adding blocked-by relationship would create cycle: %v", cycle)
		}

		b.AddBlockedBy(normalizedTargetID)
	}
	return nil
}

// removeBlockedByRelationships removes blocked-by relationships.
func removeBlockedByRelationships(tx *beancore.Tx, b *bean.Bean, targetIDs []string) {
	for _, targetID := range targetIDs {
		normalizedTargetID, _ := tx.NormalizeID(targetID)
		b.RemoveBlockedBy(normalizedTargetID)
	}
}
