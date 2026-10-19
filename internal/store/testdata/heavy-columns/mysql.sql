-- MySQL 8.0+; execute only against an explicitly disposable database.
-- Example: mysql --host=127.0.0.1 --user=fixture_admin --password --database=picklock_fixture < mysql.sql
-- Creates deterministic synthetic heavy-column datasets; never contains real images.
SET NAMES utf8mb4;
DROP TABLE IF EXISTS heavy_rows_10;
DROP TABLE IF EXISTS heavy_rows_100;
DROP TABLE IF EXISTS heavy_rows_1000;
DROP TABLE IF EXISTS heavy_rows_10000;
DROP TABLE IF EXISTS `heavy quoted`;
DROP TABLE IF EXISTS heavy_composite;
DROP TABLE IF EXISTS heavy_no_identity;

CREATE TABLE heavy_rows_10 (
  id INT NOT NULL PRIMARY KEY,
  label VARCHAR(80) CHARACTER SET utf8mb4 NOT NULL,
  text_payload LONGTEXT,
  base64_payload LONGTEXT,
  binary_payload LONGBLOB,
  json_payload JSON
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE heavy_rows_100 LIKE heavy_rows_10;
CREATE TABLE heavy_rows_1000 LIKE heavy_rows_10;
CREATE TABLE heavy_rows_10000 LIKE heavy_rows_10;
CREATE TABLE heavy_composite (
  tenant_id INT NOT NULL,
  item_id INT NOT NULL,
  label VARCHAR(80) CHARACTER SET utf8mb4 NOT NULL,
  text_payload LONGTEXT,
  PRIMARY KEY (tenant_id, item_id),
  UNIQUE KEY uq_label (label)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE heavy_no_identity (
  `select` VARCHAR(80) CHARACTER SET utf8mb4,
  text_payload LONGTEXT,
  binary_payload LONGBLOB,
  json_payload JSON
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE `heavy quoted` (
  `order` INT NOT NULL PRIMARY KEY,
  `odd name` VARCHAR(80) CHARACTER SET utf8mb4 NOT NULL,
  payload LONGTEXT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

SET SESSION cte_max_recursion_depth = 10000;
CREATE TEMPORARY TABLE fixture_numbers (n INT NOT NULL PRIMARY KEY);
INSERT INTO fixture_numbers (n)
WITH RECURSIVE seq (n) AS (
  SELECT 1
  UNION ALL
  SELECT n + 1 FROM seq WHERE n < 10000
)
SELECT n FROM seq;

INSERT INTO heavy_rows_10
SELECT n, CONCAT('row-', n),
       CASE WHEN n % 10 = 0 THEN NULL WHEN n % 10 = 1 THEN '' ELSE CONCAT('界', REPEAT('x', CASE WHEN n % 10 = 2 THEN 0 WHEN n % 10 = 3 THEN 65535 WHEN n % 10 = 4 THEN 65536 WHEN n % 10 = 5 THEN 1048576 ELSE n * 31 END)) END,
       TO_BASE64(REPEAT(CHAR(65 + n % 26), CASE WHEN n % 10 = 0 THEN 0 ELSE n * 8 END)),
       CASE WHEN n % 10 = 0 THEN NULL ELSE REPEAT(CHAR(0, 255), n * 16) END,
       JSON_OBJECT('id', n, 'text', REPEAT('j', CASE WHEN n % 10 = 0 THEN 0 ELSE n * 17 END))
FROM fixture_numbers WHERE n <= 10;
INSERT INTO heavy_rows_100 SELECT * FROM heavy_rows_10;
INSERT INTO heavy_rows_100 SELECT n, CONCAT('row-', n), CASE WHEN n % 10 = 0 THEN NULL WHEN n % 10 = 1 THEN '' ELSE CONCAT('界', REPEAT('x', (n % 8) * 512)) END, TO_BASE64(REPEAT(CHAR(65 + n % 26), (n % 5) * 192)), REPEAT(CHAR(0, 255), (n % 9) * 128), JSON_OBJECT('id', n, 'text', REPEAT('j', (n % 4) * 256)) FROM fixture_numbers WHERE n BETWEEN 11 AND 100;
INSERT INTO heavy_rows_1000 SELECT * FROM heavy_rows_100;
INSERT INTO heavy_rows_1000 SELECT n, CONCAT('row-', n), CASE WHEN n % 10 = 0 THEN NULL WHEN n % 10 = 1 THEN '' ELSE CONCAT('界', REPEAT('x', (n % 8) * 512)) END, TO_BASE64(REPEAT(CHAR(65 + n % 26), (n % 5) * 192)), REPEAT(CHAR(0, 255), (n % 9) * 128), JSON_OBJECT('id', n, 'text', REPEAT('j', (n % 4) * 256)) FROM fixture_numbers WHERE n BETWEEN 101 AND 1000;
INSERT INTO heavy_rows_10000 SELECT * FROM heavy_rows_1000;
INSERT INTO heavy_rows_10000 SELECT n, CONCAT('row-', n), CASE WHEN n % 10 = 0 THEN NULL WHEN n % 10 = 1 THEN '' ELSE CONCAT('界', REPEAT('x', (n % 8) * 512)) END, TO_BASE64(REPEAT(CHAR(65 + n % 26), (n % 5) * 192)), REPEAT(CHAR(0, 255), (n % 9) * 128), JSON_OBJECT('id', n, 'text', REPEAT('j', (n % 4) * 256)) FROM fixture_numbers WHERE n BETWEEN 1001 AND 10000;
INSERT INTO heavy_composite SELECT n DIV 100 + 1, n, CONCAT('unique-', n), CONCAT('payload-', n) FROM fixture_numbers WHERE n <= 10;
INSERT INTO heavy_no_identity SELECT CONCAT('value-', n), CASE WHEN n % 2 = 0 THEN NULL ELSE '' END, REPEAT(CHAR(255), n * 128), JSON_OBJECT('n', n) FROM fixture_numbers WHERE n <= 10;
INSERT INTO `heavy quoted` SELECT n, CONCAT('café-', n), CONCAT('quoted-', n) FROM fixture_numbers WHERE n <= 10;
DROP TEMPORARY TABLE fixture_numbers;
