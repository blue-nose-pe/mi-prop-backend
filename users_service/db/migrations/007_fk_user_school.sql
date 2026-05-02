-- Cierra el ciclo: users.school_id → school.id.
-- Ejecutar después de school (006).
-- ON DELETE NO ACTION para evitar el "ciclo de cascadas múltiples" que SQL Server rechaza
-- (school también tiene CASCADE hacia users via school.user_id).
-- En la práctica: borrar un school NO borra los users que pertenecen al colegio
--   (pone el school_id a NULL via lógica del servicio si se necesita).

IF NOT EXISTS (
    SELECT 1 FROM sys.foreign_keys WHERE name = 'fk_user_school'
)
BEGIN
    ALTER TABLE users
        ADD CONSTRAINT fk_user_school FOREIGN KEY (school_id)
            REFERENCES school(id);
END;
