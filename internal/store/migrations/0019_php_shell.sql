-- Sites of a site login: PHP's shell functions (exec, system, …) are blocked,
-- since they could reach past open_basedir to the account's other sites.
-- An admin can allow them per site.
ALTER TABLE sites ADD COLUMN block_php_shell INTEGER NOT NULL DEFAULT 0;
