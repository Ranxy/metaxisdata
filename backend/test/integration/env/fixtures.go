package env

// Fixture schema shared by the startup seed (it_app) and the per-test databases
// the runner scenarios create. Keeping the DDL here means a fixture change
// cannot drift between the harness and the tests.
//
// The "seed" variants are idempotent and keep existing rows; the "reset"
// variants drop and recreate everything and are used after the caller switched
// to (or created) the target database.

// MySQLFixtureSeedDDL seeds the fixture in the currently selected database.
const MySQLFixtureSeedDDL = `
CREATE TABLE IF NOT EXISTS users (
  id INT PRIMARY KEY,
  name VARCHAR(64) NOT NULL,
  age INT NOT NULL
);
CREATE TABLE IF NOT EXISTS orders (
  id INT PRIMARY KEY,
  user_id INT NOT NULL,
  amount DECIMAL(10,2) NOT NULL
);
CREATE OR REPLACE VIEW user_order_view AS
SELECT u.id AS user_id, u.name AS user_name, o.amount AS order_amount
FROM users u
JOIN orders o ON u.id = o.user_id;
INSERT INTO users (id, name, age) VALUES (1, 'alice', 31)
  ON DUPLICATE KEY UPDATE name = VALUES(name), age = VALUES(age);
INSERT INTO orders (id, user_id, amount) VALUES (1, 1, 9.99)
  ON DUPLICATE KEY UPDATE user_id = VALUES(user_id), amount = VALUES(amount);
`

// MySQLFixtureResetDDL drops and recreates the fixture in the currently selected
// database.
const MySQLFixtureResetDDL = `
DROP VIEW IF EXISTS user_order_view;
DROP TABLE IF EXISTS manual_sql_summary;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS users;
CREATE TABLE users (
  id INT PRIMARY KEY,
  name VARCHAR(64) NOT NULL,
  age INT NOT NULL
);
CREATE TABLE orders (
  id INT PRIMARY KEY,
  user_id INT NOT NULL,
  amount DECIMAL(10,2) NOT NULL
);
CREATE OR REPLACE VIEW user_order_view AS
SELECT u.id AS user_id, u.name AS user_name, o.amount AS order_amount
FROM users u
JOIN orders o ON u.id = o.user_id;
INSERT INTO users (id, name, age) VALUES (1, 'alice', 31);
INSERT INTO orders (id, user_id, amount) VALUES (1, 1, 9.99);
`

// PostgresFixtureSeedDDL seeds the fixture in the public schema.
const PostgresFixtureSeedDDL = `
CREATE TABLE IF NOT EXISTS public.users (
  id INT PRIMARY KEY,
  name TEXT NOT NULL,
  age INT NOT NULL
);
CREATE TABLE IF NOT EXISTS public.orders (
  id INT PRIMARY KEY,
  user_id INT NOT NULL,
  amount NUMERIC(10,2) NOT NULL
);
CREATE OR REPLACE VIEW public.user_order_view AS
SELECT u.id AS user_id, u.name AS user_name
FROM public.users u;
INSERT INTO public.users (id, name, age) VALUES (1, 'alice', 31)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, age = EXCLUDED.age;
INSERT INTO public.orders (id, user_id, amount) VALUES (1, 1, 9.99)
ON CONFLICT (id) DO UPDATE SET user_id = EXCLUDED.user_id, amount = EXCLUDED.amount;
`

// PostgresFixtureResetDDL drops and recreates the fixture in the public schema.
const PostgresFixtureResetDDL = `
DROP VIEW IF EXISTS public.user_order_view;
DROP TABLE IF EXISTS public.manual_sql_summary;
DROP TABLE IF EXISTS public.orders;
DROP TABLE IF EXISTS public.users;
CREATE TABLE public.users (
  id INT PRIMARY KEY,
  name TEXT NOT NULL,
  age INT NOT NULL
);
CREATE TABLE public.orders (
  id INT PRIMARY KEY,
  user_id INT NOT NULL,
  amount NUMERIC(10,2) NOT NULL
);
CREATE OR REPLACE VIEW public.user_order_view AS
SELECT u.id AS user_id, u.name AS user_name
FROM public.users u;
INSERT INTO public.users (id, name, age) VALUES (1, 'alice', 31);
INSERT INTO public.orders (id, user_id, amount) VALUES (1, 1, 9.99);
`
