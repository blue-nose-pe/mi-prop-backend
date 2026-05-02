-- Database: USERS (microservice) | PostgreSQL 18+
-- Requires PG 18+ for native uuidv7() (used in users and school tables).
-- Run this file connected to the 'postgres' database.

-- Idempotent create: ignore error "database already exists" on re-run.
CREATE DATABASE db_users
  ENCODING  'UTF8'
  LC_COLLATE 'en_US.UTF-8'
  LC_CTYPE   'en_US.UTF-8'
  TEMPLATE   template0;
