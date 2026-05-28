-- Filtered UNIQUE index: solo permite UN attempt ACTIVO (submitted_at IS NULL)
-- por (exam_id, user_id). Cierra una race condition donde dos requests
-- POST /api/attempts concurrentes del mismo alumno podian crear dos attempts
-- antes de que el chequeo de max_attempts_per_user los frene — el chequeo
-- lee submitted_count que NO incluye attempts activos, asi que ambos pasaban.
--
-- Con este indice, el segundo INSERT falla con 2627 (duplicate key) y el
-- handler hace retry-find + devuelve el primer attempt como Reused.
-- El primero gana por orden de commit; el segundo NO crea un attempt extra.

IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = 'uq_exam_attempt_active' AND object_id = OBJECT_ID('exam_attempt'))
    CREATE UNIQUE INDEX uq_exam_attempt_active
        ON exam_attempt (exam_id, user_id)
        WHERE submitted_at IS NULL;
