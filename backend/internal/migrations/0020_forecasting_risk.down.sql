-- Reverse 0020. The permission grants, the catalog entries, the tables and their
-- indexes are removed in dependency order.
DROP TABLE IF EXISTS risk_thresholds;
DROP TABLE IF EXISTS risk_signals;
DROP TABLE IF EXISTS forecast_runs;

DELETE FROM role_permissions WHERE permission_key IN
    ('forecast.read', 'forecast.run', 'risk.read', 'risk.manage');
DELETE FROM permissions WHERE key IN
    ('forecast.read', 'forecast.run', 'risk.read', 'risk.manage');
