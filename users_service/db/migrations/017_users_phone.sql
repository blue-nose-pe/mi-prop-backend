-- Telefono del usuario, opcional. Se sincroniza a HubSpot via UpsertContact
-- (campo `phone` del CRM). Lo guardamos local para evitar round-trip al CRM
-- cada vez que el admin lista users.
ALTER TABLE users
  ADD phone NVARCHAR(50) NULL;
