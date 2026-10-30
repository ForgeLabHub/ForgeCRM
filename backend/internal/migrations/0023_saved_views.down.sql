-- Reverse 0023. The grants, the catalog entries, the indexes and the table are
-- removed in dependency order.
DROP TABLE IF EXISTS saved_views;

DELETE FROM role_permissions WHERE permission_key IN ('views.read', 'views.manage');
DELETE FROM permissions WHERE key IN ('views.read', 'views.manage');
