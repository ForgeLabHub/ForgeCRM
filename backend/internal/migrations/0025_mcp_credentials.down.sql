DROP TABLE IF EXISTS mcp_credentials;

DELETE FROM role_permissions WHERE permission_key IN ('mcp.credentials.read', 'mcp.credentials.manage');
DELETE FROM permissions WHERE key IN ('mcp.credentials.read', 'mcp.credentials.manage');
