-- Reverse 0019. The permission grants, the catalog entries, the indexes and the
-- two tables are removed in dependency order.
DROP TABLE IF EXISTS simulation_runs;
DROP TABLE IF EXISTS simulation_scenarios;

DELETE FROM role_permissions WHERE permission_key IN
    ('simulation.read', 'simulation.manage', 'simulation.run');
DELETE FROM permissions WHERE key IN
    ('simulation.read', 'simulation.manage', 'simulation.run');
