package beangraph

import (
	"context"
	"path/filepath"

	"github.com/hmans/beans/pkg/bean"
	"github.com/hmans/beans/pkg/beangraph/model"
)

// BeanIsDirty returns whether a bean has unsaved runtime changes.
func (r *CoreResolver) BeanIsDirty(ctx context.Context, obj *bean.Bean) (bool, error) {
	return r.Core.IsDirty(obj.ID), nil
}

// BeanWorktreeID returns the worktree ID for a bean, or nil if not linked.
func (r *CoreResolver) BeanWorktreeID(ctx context.Context, obj *bean.Bean) (*string, error) {
	wtPath := r.Core.WorktreeForBean(obj.ID)
	if wtPath == "" {
		return nil, nil
	}
	// Extract worktree ID from the path (last path component)
	id := filepath.Base(wtPath)
	return &id, nil
}

// BeanParentID returns the parent ID as a pointer, or nil if no parent.
func (r *CoreResolver) BeanParentID(ctx context.Context, obj *bean.Bean) (*string, error) {
	if obj.Parent == "" {
		return nil, nil
	}
	return &obj.Parent, nil
}

// BeanBlockingIds returns the blocking IDs slice.
func (r *CoreResolver) BeanBlockingIds(ctx context.Context, obj *bean.Bean) ([]string, error) {
	return obj.Blocking, nil
}

// BeanBlockedByIds returns the blocked-by IDs slice.
func (r *CoreResolver) BeanBlockedByIds(ctx context.Context, obj *bean.Bean) ([]string, error) {
	return obj.BlockedBy, nil
}

// BeanBlockedBy resolves the full list of beans blocking this one.
// Combines both directions: the bean's own blocked_by field AND incoming
// blocking links (other beans that list this bean in their blocking field).
func (r *CoreResolver) BeanBlockedBy(ctx context.Context, obj *bean.Bean, filter *model.BeanFilter) ([]*bean.Bean, error) {
	return r.relatedBeans(r.Core.BlockedBy(obj), filter), nil
}

// BeanBlocking resolves the full list of beans this bean is blocking.
// Symmetric to BeanBlockedBy, it combines both directions: the bean's own
// blocking field AND incoming blocked_by links (other beans that list this
// bean in their blocked_by field).
func (r *CoreResolver) BeanBlocking(ctx context.Context, obj *bean.Bean, filter *model.BeanFilter) ([]*bean.Bean, error) {
	return r.relatedBeans(r.Core.Blocking(obj), filter), nil
}

// BeanParent resolves the parent bean (nil for broken links).
func (r *CoreResolver) BeanParent(ctx context.Context, obj *bean.Bean) (*bean.Bean, error) {
	return r.Core.Parent(obj), nil
}

// BeanChildren resolves the child beans.
func (r *CoreResolver) BeanChildren(ctx context.Context, obj *bean.Bean, filter *model.BeanFilter) ([]*bean.Bean, error) {
	return r.relatedBeans(r.Core.Children(obj.ID), filter), nil
}

// relatedBeans filters and sorts the beans of a relation field. The beans
// are Core's shared, read-only snapshots, like the top-level beans query.
func (r *CoreResolver) relatedBeans(beans []*bean.Bean, filter *model.BeanFilter) []*bean.Bean {
	filtered := ApplyFilter(beans, filter, r.Core)
	cfg := r.Core.Config()
	bean.SortByStatusPriorityAndType(filtered, cfg.StatusNames(), cfg.PriorityNames(), cfg.TypeNames())
	return filtered
}

// BeanImplicitStatus returns the implicit status inherited from ancestors.
func (r *CoreResolver) BeanImplicitStatus(ctx context.Context, obj *bean.Bean) (*string, error) {
	status, _ := r.Core.ImplicitStatus(obj.ID)
	if status == "" {
		return nil, nil
	}
	return &status, nil
}

// BeanImplicitStatusFrom returns the ancestor ID from which implicit status is inherited.
func (r *CoreResolver) BeanImplicitStatusFrom(ctx context.Context, obj *bean.Bean) (*string, error) {
	_, fromID := r.Core.ImplicitStatus(obj.ID)
	if fromID == "" {
		return nil, nil
	}
	return &fromID, nil
}
