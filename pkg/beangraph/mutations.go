package beangraph

import (
	"context"
	"fmt"

	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph/model"
	"github.com/hmans/beans/pkg/beancore"
	"github.com/hmans/beans/pkg/config"
)

// CreateBean creates a new bean from the given input.
func (r *CoreResolver) CreateBean(ctx context.Context, input model.CreateBeanInput) (*bean.Bean, error) {
	b := &bean.Bean{
		Slug:     bean.Slugify(input.Title),
		Title:    input.Title,
		Blocking: []string{},
	}

	// Configured defaults apply to every client (CLI, TUI, web UI, raw GraphQL)
	b.Status, b.Type = config.DefaultStatus, config.DefaultType
	if cfg := r.Core.Config(); cfg != nil {
		b.Status, b.Type = cfg.GetDefaultStatus(), cfg.GetDefaultType()
	}

	// Optional fields with defaults documented in schema
	if input.Type != nil {
		b.Type = *input.Type
	}
	if input.Status != nil {
		b.Status = *input.Status
	}
	if input.Priority != nil {
		b.Priority = *input.Priority
	}
	if input.Body != nil {
		b.Body = *input.Body
	}
	if len(input.Tags) > 0 {
		b.Tags = input.Tags
	}

	createOpts := []beancore.UpdateOption{
		// Relations are normalised and validated under Core's write lock, so
		// the beans they refer to can't change or disappear meanwhile.
		beancore.WithValidation(func(tx *beancore.Tx, b *bean.Bean) error {
			return applyCreateRelations(tx, b, input)
		}),
	}
	// Handle custom prefix - Core generates a unique ID with it
	if input.Prefix != nil && *input.Prefix != "" {
		createOpts = append(createOpts, beancore.WithIDPrefix(*input.Prefix))
	}

	if err := r.Core.Create(b, createOpts...); err != nil {
		return nil, err
	}

	return b, nil
}

// applyCreateRelations validates the parent, blocking and blocked-by
// relations of a new bean and sets them (with normalised IDs) on b.
func applyCreateRelations(tx *beancore.Tx, b *bean.Bean, input model.CreateBeanInput) error {
	// Handle parent (with validation)
	if input.Parent != nil && *input.Parent != "" {
		// Normalise short ID to full ID
		parentID, _ := tx.NormalizeID(*input.Parent)
		if err := tx.ValidateParent(b, parentID); err != nil {
			return err
		}
		b.Parent = parentID
	}

	// Handle blocking (with validation)
	if len(input.Blocking) > 0 {
		// Normalise short IDs to full IDs
		normalizedBlocking := make([]string, len(input.Blocking))
		for i, id := range input.Blocking {
			normalizedBlocking[i], _ = tx.NormalizeID(id)
			// Verify target exists
			if _, ok := tx.Get(normalizedBlocking[i]); !ok {
				return fmt.Errorf("target bean not found: %s", id)
			}
		}
		b.Blocking = normalizedBlocking
	}

	// Handle blocked_by (with cycle validation)
	if len(input.BlockedBy) > 0 {
		// Normalise short IDs to full IDs
		normalizedBlockedBy := make([]string, len(input.BlockedBy))
		for i, id := range input.BlockedBy {
			normalizedBlockedBy[i], _ = tx.NormalizeID(id)
			// Verify blocker exists
			if _, ok := tx.Get(normalizedBlockedBy[i]); !ok {
				return fmt.Errorf("blocker bean not found: %s", id)
			}
		}
		// Check for cycles with blocking relationships
		// (new bean being blocked_by X means X→newBean, check if newBean→X exists via blocking)
		for _, blockerID := range normalizedBlockedBy {
			for _, blockingID := range b.Blocking {
				if blockerID == blockingID {
					return fmt.Errorf("would create cycle: new bean both blocks and is blocked by %s", blockerID)
				}
			}
		}
		b.BlockedBy = normalizedBlockedBy
	}
	return nil
}

// UpdateBean updates an existing bean. The input is applied to the current
// version and validated under Core's write lock (see beancore.Core.Mutate):
// if ifMatch or any validation fails, the bean is left unchanged.
func (r *CoreResolver) UpdateBean(ctx context.Context, id string, input model.UpdateBeanInput, opts ...beancore.UpdateOption) (*bean.Bean, error) {
	// Validate body and bodyMod are mutually exclusive
	if input.Body != nil && input.BodyMod != nil {
		return nil, fmt.Errorf("cannot specify both body and bodyMod")
	}

	// Validate tags and addTags/removeTags are mutually exclusive
	if input.Tags != nil && (input.AddTags != nil || input.RemoveTags != nil) {
		return nil, fmt.Errorf("cannot specify both tags and addTags/removeTags")
	}

	// If the bean is linked to a worktree, Core auto-routes the write there.
	return r.Core.Mutate(id, input.IfMatch, func(tx *beancore.Tx, b *bean.Bean) error {
		return applyUpdate(tx, b, input)
	}, opts...)
}

// applyUpdate applies an update input to b, validating relationship changes.
func applyUpdate(tx *beancore.Tx, b *bean.Bean, input model.UpdateBeanInput) error {
	// Update fields if provided
	if input.Title != nil {
		b.Title = *input.Title
	}
	if input.Status != nil {
		b.Status = *input.Status
	}
	if input.Type != nil {
		b.Type = *input.Type
	}
	if input.Priority != nil {
		b.Priority = *input.Priority
	}
	if input.Order != nil {
		b.Order = *input.Order
	}
	if input.Body != nil {
		b.Body = *input.Body
	} else if input.BodyMod != nil {
		// Apply body modifications
		workingBody := b.Body

		// Apply replacements sequentially
		for i, replaceOp := range input.BodyMod.Replace {
			newBody, err := bean.ReplaceOnce(workingBody, replaceOp.Old, replaceOp.New)
			if err != nil {
				return fmt.Errorf("replacement %d failed: %w", i, err)
			}
			workingBody = newBody
		}

		// Apply append if provided
		if input.BodyMod.Append != nil && *input.BodyMod.Append != "" {
			workingBody = bean.AppendWithSeparator(workingBody, *input.BodyMod.Append)
		}

		b.Body = workingBody
	}
	// Handle tags
	if input.Tags != nil {
		b.Tags = input.Tags
	} else if input.AddTags != nil || input.RemoveTags != nil {
		// Build a set of current tags
		tagSet := make(map[string]bool)
		for _, tag := range b.Tags {
			tagSet[tag] = true
		}

		// Add new tags
		for _, tag := range input.AddTags {
			tagSet[tag] = true
		}

		// Remove tags
		for _, tag := range input.RemoveTags {
			delete(tagSet, tag)
		}

		// Convert back to slice
		newTags := make([]string, 0, len(tagSet))
		for tag := range tagSet {
			newTags = append(newTags, tag)
		}
		b.Tags = newTags
	}

	// Handle parent relationship
	if input.Parent != nil {
		if err := validateAndSetParent(tx, b, *input.Parent); err != nil {
			return err
		}
	}

	// Handle blocking relationships
	if input.AddBlocking != nil {
		if err := validateAndAddBlocking(tx, b, input.AddBlocking); err != nil {
			return err
		}
	}
	if input.RemoveBlocking != nil {
		removeBlockingRelationships(tx, b, input.RemoveBlocking)
	}

	// Handle blocked-by relationships
	if input.AddBlockedBy != nil {
		if err := validateAndAddBlockedBy(tx, b, input.AddBlockedBy); err != nil {
			return err
		}
	}
	if input.RemoveBlockedBy != nil {
		removeBlockedByRelationships(tx, b, input.RemoveBlockedBy)
	}
	return nil
}

// DeleteBean removes a bean and its incoming links.
func (r *CoreResolver) DeleteBean(ctx context.Context, id string) (bool, error) {
	// Verify bean exists
	_, err := r.Core.Get(id)
	if err != nil {
		return false, err
	}

	// Remove incoming links first
	if _, err := r.Core.RemoveLinksTo(id); err != nil {
		return false, err
	}

	// Delete the bean
	if err := r.Core.Delete(id); err != nil {
		return false, err
	}

	return true, nil
}

// SetParent sets or clears the parent of a bean.
func (r *CoreResolver) SetParent(ctx context.Context, id string, parentID *string, ifMatch *string) (*bean.Bean, error) {
	return r.Core.Mutate(id, ifMatch, func(tx *beancore.Tx, b *bean.Bean) error {
		newParent := ""
		if parentID != nil {
			// Normalise short ID to full ID
			newParent, _ = tx.NormalizeID(*parentID)
		}

		// Validate parent type hierarchy
		if newParent != "" {
			if err := tx.ValidateParent(b, newParent); err != nil {
				return err
			}
			// Check for cycles
			if cycle := tx.DetectCycle(b.ID, "parent", newParent); cycle != nil {
				return fmt.Errorf("would create cycle: %v", cycle)
			}
		}

		b.Parent = newParent
		return nil
	})
}

// AddBlocking adds a blocking relationship.
func (r *CoreResolver) AddBlocking(ctx context.Context, id string, targetID string, ifMatch *string) (*bean.Bean, error) {
	return r.Core.Mutate(id, ifMatch, func(tx *beancore.Tx, b *bean.Bean) error {
		// Normalise short ID to full ID
		normalizedTargetID, _ := tx.NormalizeID(targetID)

		if normalizedTargetID == b.ID {
			return fmt.Errorf("bean cannot block itself")
		}

		// Check target exists
		if _, ok := tx.Get(normalizedTargetID); !ok {
			return fmt.Errorf("target bean not found: %s", targetID)
		}

		// Check for cycles in both directions
		if cycle := tx.DetectCycle(b.ID, "blocking", normalizedTargetID); cycle != nil {
			return fmt.Errorf("would create cycle: %v", cycle)
		}
		if cycle := tx.DetectCycle(normalizedTargetID, "blocked_by", b.ID); cycle != nil {
			return fmt.Errorf("would create cycle: %v", cycle)
		}

		b.AddBlocking(normalizedTargetID)
		return nil
	})
}

// RemoveBlocking removes a blocking relationship.
func (r *CoreResolver) RemoveBlocking(ctx context.Context, id string, targetID string, ifMatch *string) (*bean.Bean, error) {
	return r.Core.Mutate(id, ifMatch, func(tx *beancore.Tx, b *bean.Bean) error {
		normalizedTargetID, _ := tx.NormalizeID(targetID)
		b.RemoveBlocking(normalizedTargetID)
		return nil
	})
}

// AddBlockedBy adds a blocked-by relationship.
func (r *CoreResolver) AddBlockedBy(ctx context.Context, id string, targetID string, ifMatch *string) (*bean.Bean, error) {
	return r.Core.Mutate(id, ifMatch, func(tx *beancore.Tx, b *bean.Bean) error {
		normalizedTargetID, _ := tx.NormalizeID(targetID)

		if normalizedTargetID == b.ID {
			return fmt.Errorf("bean cannot be blocked by itself")
		}

		// Check target exists
		if _, ok := tx.Get(normalizedTargetID); !ok {
			return fmt.Errorf("blocker bean not found: %s", targetID)
		}

		// Check for cycles in both directions
		if cycle := tx.DetectCycle(normalizedTargetID, "blocking", b.ID); cycle != nil {
			return fmt.Errorf("would create cycle: %v", cycle)
		}
		if cycle := tx.DetectCycle(b.ID, "blocked_by", normalizedTargetID); cycle != nil {
			return fmt.Errorf("would create cycle: %v", cycle)
		}

		b.AddBlockedBy(normalizedTargetID)
		return nil
	})
}

// RemoveBlockedBy removes a blocked-by relationship.
func (r *CoreResolver) RemoveBlockedBy(ctx context.Context, id string, targetID string, ifMatch *string) (*bean.Bean, error) {
	return r.Core.Mutate(id, ifMatch, func(tx *beancore.Tx, b *bean.Bean) error {
		normalizedTargetID, _ := tx.NormalizeID(targetID)
		b.RemoveBlockedBy(normalizedTargetID)
		return nil
	})
}

// ArchiveBean archives a bean.
func (r *CoreResolver) ArchiveBean(ctx context.Context, id string) (bool, error) {
	if err := r.Core.Archive(id); err != nil {
		return false, err
	}
	return true, nil
}
