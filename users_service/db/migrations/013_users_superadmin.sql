-- Bootstrap del primer superadmin + flujo de cambio de password forzado.
--
--   * is_superadmin: BYPASSA el chequeo de permisos. SOLO los superadmins
--     pueden editar la tabla `permission` (creación de catálogo de permisos).
--     Cualquier otro permiso granular es chequeable como antes.
--
--   * must_change_password: TRUE cuando el user fue creado por el sistema
--     (bootstrap) o por un admin con password temporal. El cliente DEBE
--     cambiarla en el primer login antes de poder operar — el handler
--     /api/auth/login devuelve una flag, y todas las rutas (excepto
--     /api/users/me y /api/users/me/change-password) responden 403
--     PASSWORD_CHANGE_REQUIRED si must_change_password=1.

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('users') AND name = 'is_superadmin'
)
BEGIN
    ALTER TABLE users ADD is_superadmin BIT NOT NULL CONSTRAINT df_users_is_superadmin DEFAULT 0;
END;

IF NOT EXISTS (
    SELECT 1 FROM sys.columns
     WHERE object_id = OBJECT_ID('users') AND name = 'must_change_password'
)
BEGIN
    ALTER TABLE users ADD must_change_password BIT NOT NULL CONSTRAINT df_users_must_change_password DEFAULT 0;
END;

-- Índice para chequeo rápido "¿hay algún superadmin?" (lo usa el bootstrap).
IF NOT EXISTS (
    SELECT 1 FROM sys.indexes
     WHERE name = 'idx_users_superadmin' AND object_id = OBJECT_ID('users')
)
    CREATE INDEX idx_users_superadmin ON users (is_superadmin) WHERE is_superadmin = 1;
