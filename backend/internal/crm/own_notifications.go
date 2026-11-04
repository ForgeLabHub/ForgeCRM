package crm

import (
	"context"

	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// ListMyNotifications returns the caller's own recent notifications. The list is
// already narrowed to the caller by ListNotifications; this wrapper adds the
// permission check and the active-membership lock, so the bot cannot read a
// notification for a user who has been disabled.
func (s *Service) ListMyNotifications(ctx context.Context, p *rbac.Principal, unreadOnly bool, limit int) ([]Notification, error) {
	if p == nil || !p.Has(rbac.PermNotificationsRead) {
		return nil, rbac.ErrForbidden
	}
	if limit <= 0 {
		return nil, ErrNotFound
	}
	var items []Notification
	err := s.db.WithTx(ctx, func(ctx context.Context, _ database.Tx) error {
		if err := s.lockActiveMember(ctx, p); err != nil {
			return err
		}
		var err error
		items, _, err = s.ListNotifications(ctx, p, unreadOnly, limit, 0)
		return err
	})
	return items, err
}
