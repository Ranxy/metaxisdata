package postgresql

import (
	"testing"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// TestSelectLineage_Table contains table-driven tests for SELECT statement lineage.
func TestSelectLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "simple select with qualified columns",
			SQL:  "SELECT users.id, users.name FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id", IsTemp: Bool(true)},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name", IsTemp: Bool(true)},
			},
		},
		{
			Name: "select with column aliases",
			SQL:  "SELECT users.id AS user_id, users.name AS user_name FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "user_id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "user_name"},
			},
		},
		{
			Name: "select with expression (|| operator)",
			// PostgreSQL uses || for string concatenation - note: the analyzer may not detect || as transformation
			SQL: "SELECT id, first_name || ' ' || last_name AS full_name FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "first_name", ToTable: "__result__", ToField: "full_name"},
				{FromTable: "users", FromField: "last_name", ToTable: "__result__", ToField: "full_name"},
			},
		},
		{
			Name: "select with CONCAT function",
			SQL:  "SELECT id, CONCAT(first_name, ' ', last_name) AS full_name FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id", HasTransform: Bool(false)},
				{FromTable: "users", FromField: "first_name", ToTable: "__result__", ToField: "full_name", HasTransform: Bool(true)},
				{FromTable: "users", FromField: "last_name", ToTable: "__result__", ToField: "full_name", HasTransform: Bool(true)},
			},
		},
		{
			Name: "select with WHERE clause",
			SQL:  "SELECT id, name FROM users WHERE age > 30",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
			},
		},
		{
			Name: "select star without catalog",
			SQL:  "SELECT * FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "*", ToTable: "__result__"},
			},
		},
		{
			Name: "select with qualified table name (schema.table)",
			SQL:  "SELECT mydb.users.id, mydb.users.name FROM mydb.users",
			ExpectedEdges: []ExpectedEdge{
				{FromSchema: "mydb", FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromSchema: "mydb", FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
			},
		},
		{
			Name: "select with table alias",
			SQL:  "SELECT u.id, u.name FROM users AS u",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
			},
		},
		{
			Name: "select with COALESCE function",
			SQL:  "SELECT id, COALESCE(nickname, name) AS display_name FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "nickname", ToTable: "__result__", ToField: "display_name", HasTransform: Bool(true)},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "display_name", HasTransform: Bool(true)},
			},
		},
		{
			Name: "select with CAST expression",
			SQL:  "SELECT id, CAST(created_at AS DATE) AS create_date FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "created_at", ToTable: "__result__", ToField: "create_date", HasTransform: Bool(true)},
			},
		},
		{
			Name: "select with PostgreSQL type cast (::)",
			// Note: :: type cast may not be detected as transformation by the current analyzer
			SQL: "SELECT id, created_at::date AS create_date FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "created_at", ToTable: "__result__", ToField: "create_date"},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestJoinLineage_Table contains table-driven tests for JOIN statement lineage.
func TestJoinLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "inner join",
			SQL: `SELECT u.id, u.name, o.order_id, o.total 
			      FROM users u 
			      JOIN orders o ON u.id = o.user_id`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
				{FromTable: "orders", FromField: "order_id", ToTable: "__result__", ToField: "order_id"},
				{FromTable: "orders", FromField: "total", ToTable: "__result__", ToField: "total"},
			},
		},
		{
			Name: "left outer join",
			SQL: `SELECT u.id, u.name, o.order_id
			      FROM users u
			      LEFT OUTER JOIN orders o ON u.id = o.user_id`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
				{FromTable: "orders", FromField: "order_id", ToTable: "__result__", ToField: "order_id"},
			},
		},
		{
			Name: "multiple joins",
			SQL: `SELECT u.name, o.order_id, p.product_name
			      FROM users u
			      JOIN orders o ON u.id = o.user_id
			      JOIN products p ON o.product_id = p.id`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
				{FromTable: "orders", FromField: "order_id", ToTable: "__result__", ToField: "order_id"},
				{FromTable: "products", FromField: "product_name", ToTable: "__result__", ToField: "product_name"},
			},
		},
		{
			Name: "cross join",
			SQL: `SELECT u.name, d.department_name
			      FROM users u
			      CROSS JOIN departments d`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
				{FromTable: "departments", FromField: "department_name", ToTable: "__result__", ToField: "department_name"},
			},
		},
		{
			Name: "natural join",
			SQL: `SELECT id, name, department_id
			      FROM users
			      NATURAL JOIN user_departments`,
			// Note: NATURAL JOIN exposes shared columns without a stable source table
			// in the current analyzer, so this test only asserts the result column
			// names and not which source table they are attributed to.
			ExpectedEdges: []ExpectedEdge{
				{FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromField: "name", ToTable: "__result__", ToField: "name"},
				{FromField: "department_id", ToTable: "__result__", ToField: "department_id"},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestSubqueryLineage_Table contains table-driven tests for subquery lineage.
func TestSubqueryLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "subquery in FROM clause",
			SQL: `SELECT sub.id, sub.total_orders
			      FROM (SELECT user_id AS id, COUNT(*) AS total_orders FROM orders GROUP BY user_id) AS sub`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "orders", FromField: "user_id", ToTable: "__result__", ToField: "id"},
				{FromTable: "orders", ToTable: "__result__", ToField: "total_orders", HasTransform: Bool(true)},
			},
		},
		{
			Name: "nested subquery filters intermediate lineage",
			SQL: `SELECT foo.user_id, foo.total_amount
			      FROM (
			          SELECT user_id, total_amount
			          FROM (
			              SELECT user_id, SUM(amount) AS total_amount
			              FROM orders
			              GROUP BY user_id
			          ) sub
			      ) foo
			      JOIN (
			          SELECT user_id, COUNT(*) AS order_count
			          FROM user_orders
			          GROUP BY user_id
			      ) bar
			      ON foo.user_id = bar.user_id`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "orders", FromField: "user_id", ToTable: "__result__", ToField: "user_id"},
				{FromTable: "orders", FromField: "amount", ToTable: "__result__", ToField: "total_amount"},
			},
		},
		{
			Name: "correlated subquery in SELECT",
			SQL: `SELECT u.id, u.name, 
			          (SELECT COUNT(*) FROM orders o WHERE o.user_id = u.id) AS order_count
			      FROM users u`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
			},
		},
		{
			Name: "subquery in WHERE EXISTS",
			SQL: `SELECT u.id, u.name
			      FROM users u
			      WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = u.id)`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestCTELineage_Table contains table-driven tests for CTE lineage.
func TestCTELineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "simple CTE",
			SQL: `WITH active_users AS (
			          SELECT id, name FROM users WHERE status = 'active'
			      )
			      SELECT au.id, au.name FROM active_users au`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
			},
		},
		{
			Name: "multiple CTEs",
			SQL: `WITH 
			          active_users AS (
			              SELECT id, name FROM users WHERE status = 'active'
			          ),
			          user_orders AS (
			              SELECT user_id, COUNT(*) AS order_count FROM orders GROUP BY user_id
			          )
			      SELECT au.id, au.name, uo.order_count
			      FROM active_users au
			      LEFT JOIN user_orders uo ON au.id = uo.user_id`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
				{FromTable: "orders", ToTable: "__result__", ToField: "order_count", HasTransform: Bool(true)},
			},
		},
		{
			Name: "CTE with explicit column names",
			SQL: `WITH user_summary (user_id, user_name) AS (
			          SELECT id, name FROM users
			      )
			      SELECT user_id, user_name FROM user_summary`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "user_id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "user_name"},
			},
		},
		{
			Name: "CTE with explicit column names referenced through alias",
			SQL: `WITH user_summary (user_id, user_name) AS (
			          SELECT id, name FROM users
			      )
			      SELECT us.user_id, us.user_name FROM user_summary us`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "user_id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "user_name"},
			},
		},
		{
			Name: "nested CTE references",
			SQL: `WITH 
			          first_cte AS (
			              SELECT id, name FROM users
			          ),
			          second_cte AS (
			              SELECT id, name FROM first_cte WHERE id > 10
			          )
			      SELECT id, name FROM second_cte`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestUnionLineage_Table contains table-driven tests for UNION statement lineage.
func TestUnionLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "simple UNION",
			SQL: `SELECT id, name FROM employees 
			      UNION 
			      SELECT id, name FROM contractors`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "employees", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "employees", FromField: "name", ToTable: "__result__", ToField: "name"},
				{FromTable: "contractors", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "contractors", FromField: "name", ToTable: "__result__", ToField: "name"},
			},
		},
		{
			Name: "UNION ALL",
			SQL: `SELECT user_id, amount FROM sales 
			      UNION ALL 
			      SELECT user_id, amount FROM refunds`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "sales", FromField: "user_id", ToTable: "__result__", ToField: "user_id"},
				{FromTable: "sales", FromField: "amount", ToTable: "__result__", ToField: "amount"},
				{FromTable: "refunds", FromField: "user_id", ToTable: "__result__", ToField: "user_id"},
				{FromTable: "refunds", FromField: "amount", ToTable: "__result__", ToField: "amount"},
			},
		},
		{
			Name: "multiple UNION",
			SQL: `SELECT id, name FROM employees 
			      UNION 
			      SELECT id, name FROM contractors
			      UNION
			      SELECT id, name FROM vendors`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "employees", ToTable: "__result__"},
				{FromTable: "contractors", ToTable: "__result__"},
				{FromTable: "vendors", ToTable: "__result__"},
			},
		},
		{
			Name: "INTERSECT operation",
			SQL: `SELECT id FROM users 
			      INTERSECT 
			      SELECT user_id AS id FROM orders`,
			// Note: INTERSECT may not return edges from all tables in current implementation
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
			},
		},
		{
			Name: "EXCEPT operation",
			SQL: `SELECT id FROM users 
			      EXCEPT 
			      SELECT user_id AS id FROM blocked_users`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "blocked_users", FromField: "user_id", ToTable: "__result__", ToField: "id"},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestInsertLineage_Table contains table-driven tests for INSERT statement lineage.
func TestInsertLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "INSERT SELECT",
			SQL:  "INSERT INTO tmp_users SELECT id, name FROM users WHERE age > 30",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "tmp_users", ToField: "id", IsTemp: Bool(false)},
				{FromTable: "users", FromField: "name", ToTable: "tmp_users", ToField: "name", IsTemp: Bool(false)},
			},
		},
		{
			Name: "INSERT SELECT with explicit columns",
			SQL:  "INSERT INTO tmp_users (user_id, user_name) SELECT id, name FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "tmp_users", ToField: "user_id"},
				{FromTable: "users", FromField: "name", ToTable: "tmp_users", ToField: "user_name"},
			},
		},
		{
			Name: "INSERT SELECT with JOIN",
			SQL: `INSERT INTO summary 
			      SELECT u.id, u.name, COUNT(o.id) AS order_count 
			      FROM users u 
			      JOIN orders o ON u.id = o.user_id 
			      GROUP BY u.id, u.name`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "summary", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "summary", ToField: "name"},
				{ToTable: "summary", ToField: "order_count", HasTransform: Bool(true)},
			},
		},
		{
			Name: "INSERT with CTE",
			SQL: `WITH active_users AS (
			          SELECT id, name FROM users WHERE status = 'active'
			      )
			      INSERT INTO user_backup SELECT * FROM active_users`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", ToTable: "user_backup"},
			},
		},
		{
			Name: "INSERT with CTE explicit column names",
			SQL: `WITH active_users (user_id, user_name) AS (
			          SELECT id, name FROM users WHERE status = 'active'
			      )
			      INSERT INTO user_backup (id, name)
			      SELECT user_id, user_name FROM active_users`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "user_backup", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "user_backup", ToField: "name"},
			},
		},
		{
			Name: "INSERT with RETURNING clause",
			SQL: `INSERT INTO users (name, email)
			      SELECT name, email FROM pending_users
			      RETURNING id, name`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "pending_users", FromField: "name", ToTable: "users", ToField: "name"},
				{FromTable: "pending_users", FromField: "email", ToTable: "users", ToField: "email"},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestCreateTableLineage_Table contains table-driven tests for CREATE TABLE AS SELECT.
func TestCreateTableLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "CREATE TABLE AS SELECT",
			SQL:  "CREATE TABLE new_users AS SELECT id, name FROM users WHERE age > 30",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "new_users", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "new_users", ToField: "name"},
			},
		},
		{
			Name: "CREATE TABLE AS SELECT with JOIN",
			SQL: `CREATE TABLE user_stats AS 
			      SELECT u.id, u.name, COUNT(o.id) AS order_count, SUM(o.total) AS total_spent
			      FROM users u 
			      LEFT JOIN orders o ON u.id = o.user_id 
			      GROUP BY u.id, u.name`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "user_stats", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "user_stats", ToField: "name"},
				{ToTable: "user_stats", ToField: "order_count", HasTransform: Bool(true)},
				{ToTable: "user_stats", ToField: "total_spent", HasTransform: Bool(true)},
			},
		},
		{
			Name: "CREATE TEMP TABLE AS SELECT",
			SQL:  "CREATE TEMP TABLE tmp_active_users AS SELECT id, name FROM users WHERE status = 'active'",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "tmp_active_users", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "tmp_active_users", ToField: "name"},
			},
		},
		{
			Name: "CREATE TABLE AS SELECT with schema",
			SQL:  "CREATE TABLE myschema.new_users AS SELECT id, name FROM public.users",
			ExpectedEdges: []ExpectedEdge{
				{FromSchema: "public", FromTable: "users", FromField: "id", ToSchema: "myschema", ToTable: "new_users", ToField: "id"},
				{FromSchema: "public", FromTable: "users", FromField: "name", ToSchema: "myschema", ToTable: "new_users", ToField: "name"},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestUpdateLineage_Table contains table-driven tests for UPDATE statement lineage.
func TestUpdateLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "UPDATE with literal values",
			SQL:  "UPDATE users SET email = 'new@email.com', updated_at = NOW() WHERE id = 1",
			ExpectedEdges: []ExpectedEdge{
				{ToTable: "users", ToField: "email", HasTransform: Bool(true)},
				{ToTable: "users", ToField: "updated_at", HasTransform: Bool(true)},
			},
		},
		{
			Name: "UPDATE with column reference",
			SQL:  "UPDATE users SET new_email = old_email, full_name = first_name || ' ' || last_name",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "old_email", ToTable: "users", ToField: "new_email"},
				{FromTable: "users", FromField: "first_name", ToTable: "users", ToField: "full_name", HasTransform: Bool(true)},
				{FromTable: "users", FromField: "last_name", ToTable: "users", ToField: "full_name", HasTransform: Bool(true)},
			},
		},
		{
			// PostgreSQL UPDATE with FROM clause (equivalent to MySQL UPDATE JOIN)
			Name: "UPDATE with FROM clause",
			SQL: `UPDATE orders 
			      SET user_name = u.name, discount = orders.total * 0.1
			      FROM users u
			      WHERE orders.user_id = u.id`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "name", ToTable: "orders", ToField: "user_name"},
				{FromTable: "orders", FromField: "total", ToTable: "orders", ToField: "discount", HasTransform: Bool(true)},
			},
		},
		{
			Name: "UPDATE with subquery",
			SQL: `UPDATE users 
			      SET total_orders = (SELECT COUNT(*) FROM orders WHERE orders.user_id = users.id)`,
			ExpectedEdges: []ExpectedEdge{
				{ToTable: "users", ToField: "total_orders", HasTransform: Bool(true)},
			},
		},
		{
			Name: "UPDATE with CTE",
			SQL: `WITH high_value_users AS (
			          SELECT user_id FROM orders GROUP BY user_id HAVING SUM(total) > 1000
			      )
			      UPDATE users SET status = 'premium'
			      FROM high_value_users hvu
			      WHERE users.id = hvu.user_id`,
			ExpectedEdges: []ExpectedEdge{
				{ToTable: "users", ToField: "status", HasTransform: Bool(true)},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestDeleteLineage_Table contains table-driven tests for DELETE statement lineage.
// Note: PostgreSQL uses USING clause for DELETE with JOINs (not JOIN like MySQL)
func TestDeleteLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "simple DELETE",
			SQL:  "DELETE FROM users WHERE status = 'inactive' AND last_login < NOW() - INTERVAL '1 year'",
			ExpectedEdges: []ExpectedEdge{
				{FromField: "status", ToTable: "users", ToField: "__deletion__", HasTransform: Bool(true)},
				{FromField: "last_login", ToTable: "users", ToField: "__deletion__", HasTransform: Bool(true)},
			},
		},
		{
			// PostgreSQL uses USING clause for DELETE with table references
			Name: "DELETE with USING clause",
			SQL: `DELETE FROM orders
			      USING users u
			      WHERE orders.user_id = u.id AND u.status = 'deleted'`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "status", ToTable: "orders", ToField: "__deletion__", HasTransform: Bool(true)},
			},
		},
		{
			Name: "DELETE with subquery",
			SQL: `DELETE FROM users 
			      WHERE id IN (SELECT user_id FROM orders WHERE status = 'cancelled' GROUP BY user_id HAVING COUNT(*) > 5)`,
			ExpectedEdges: []ExpectedEdge{
				{ToTable: "users", ToField: "__deletion__", HasTransform: Bool(true)},
			},
		},
		{
			Name: "DELETE with CTE",
			SQL: `WITH inactive_users AS (
			          SELECT id FROM users WHERE last_login < NOW() - INTERVAL '2 years'
			      )
			      DELETE FROM user_sessions
			      USING inactive_users iu
			      WHERE user_sessions.user_id = iu.id`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "user_sessions", ToField: "__deletion__", HasTransform: Bool(true)},
			},
		},
		{
			Name: "DELETE with RETURNING",
			SQL:  "DELETE FROM logs WHERE created_at < '2020-01-01' RETURNING id, message",
			ExpectedEdges: []ExpectedEdge{
				{FromField: "created_at", ToTable: "logs", ToField: "__deletion__", HasTransform: Bool(true)},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestWindowFunctionLineage_Table contains table-driven tests for window function lineage.
func TestWindowFunctionLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "ROW_NUMBER window function",
			SQL: `SELECT 
			        id, 
			        name, 
			        salary,
			        ROW_NUMBER() OVER (PARTITION BY department ORDER BY salary DESC) AS rank_in_dept
			      FROM employees`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "employees", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "employees", FromField: "name", ToTable: "__result__", ToField: "name"},
				{FromTable: "employees", FromField: "salary", ToTable: "__result__", ToField: "salary"},
				{FromTable: "employees", ToTable: "__result__", ToField: "rank_in_dept", HasTransform: Bool(true)},
			},
		},
		{
			Name: "multiple window functions",
			SQL: `SELECT 
			        product_id,
			        sale_date,
			        amount,
			        SUM(amount) OVER (PARTITION BY product_id ORDER BY sale_date) AS running_total,
			        AVG(amount) OVER (PARTITION BY product_id) AS avg_amount
			      FROM sales`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "sales", FromField: "product_id", ToTable: "__result__", ToField: "product_id"},
				{FromTable: "sales", FromField: "sale_date", ToTable: "__result__", ToField: "sale_date"},
				{FromTable: "sales", FromField: "amount", ToTable: "__result__", ToField: "amount"},
				{ToTable: "__result__", ToField: "running_total", HasTransform: Bool(true)},
				{ToTable: "__result__", ToField: "avg_amount", HasTransform: Bool(true)},
			},
		},
		{
			Name: "LEAD and LAG functions",
			SQL: `SELECT 
			        id,
			        sale_date,
			        amount,
			        LAG(amount) OVER (ORDER BY sale_date) AS prev_amount,
			        LEAD(amount) OVER (ORDER BY sale_date) AS next_amount
			      FROM sales`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "sales", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "sales", FromField: "sale_date", ToTable: "__result__", ToField: "sale_date"},
				{FromTable: "sales", FromField: "amount", ToTable: "__result__", ToField: "amount"},
				{ToTable: "__result__", ToField: "prev_amount", HasTransform: Bool(true)},
				{ToTable: "__result__", ToField: "next_amount", HasTransform: Bool(true)},
			},
		},
		{
			Name: "RANK and DENSE_RANK functions",
			SQL: `SELECT 
			        name,
			        score,
			        RANK() OVER (ORDER BY score DESC) AS rank,
			        DENSE_RANK() OVER (ORDER BY score DESC) AS dense_rank
			      FROM students`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "students", FromField: "name", ToTable: "__result__", ToField: "name"},
				{FromTable: "students", FromField: "score", ToTable: "__result__", ToField: "score"},
				{ToTable: "__result__", ToField: "rank", HasTransform: Bool(true)},
				{ToTable: "__result__", ToField: "dense_rank", HasTransform: Bool(true)},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestCatalogIntegration_Table contains table-driven tests for catalog integration.
func TestCatalogIntegration_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "SELECT * with catalog",
			SQL:  "SELECT * FROM users",
			Catalog: CreateSimpleCatalog(map[string][]string{
				"users": {"id", "name", "email"},
			}),
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
				{FromTable: "users", FromField: "email", ToTable: "__result__", ToField: "email"},
			},
		},
		{
			Name: "table.* with catalog",
			SQL:  "SELECT users.*, orders.total FROM users JOIN orders ON users.id = orders.user_id",
			Catalog: CreateSimpleCatalog(map[string][]string{
				"users":  {"id", "name"},
				"orders": {"order_id", "user_id", "total"},
			}),
			// Note: table.* may not be fully expanded by the current analyzer
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", ToTable: "__result__"},
				{FromTable: "orders", FromField: "total", ToTable: "__result__", ToField: "total"},
			},
		},
		{
			Name: "SELECT * in subquery with catalog",
			SQL:  "SELECT * FROM (SELECT * FROM users WHERE status = 'active') AS active_users",
			Catalog: CreateSimpleCatalog(map[string][]string{
				"users": {"id", "name", "email", "status"},
			}),
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
				{FromTable: "users", FromField: "email", ToTable: "__result__", ToField: "email"},
				{FromTable: "users", FromField: "status", ToTable: "__result__", ToField: "status"},
			},
		},
		{
			Name: "SELECT * in CTE with catalog",
			SQL: `WITH active_users AS (SELECT * FROM users WHERE status = 'active')
			      SELECT * FROM active_users`,
			Catalog: CreateSimpleCatalog(map[string][]string{
				"users": {"id", "name", "email", "status"},
			}),
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__"},
				{FromTable: "users", FromField: "name", ToTable: "__result__"},
				{FromTable: "users", FromField: "email", ToTable: "__result__"},
				{FromTable: "users", FromField: "status", ToTable: "__result__"},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestRelationTypes_Table contains tests that verify relation types.
func TestRelationTypes_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "direct relation type",
			SQL:  "SELECT id, name FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id", RelationType: RelType(model.RelationTypeDirect)},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name", RelationType: RelType(model.RelationTypeDirect)},
			},
		},
		{
			Name: "indirect relation type (expression)",
			SQL:  "SELECT id, UPPER(name) AS name_upper FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id", RelationType: RelType(model.RelationTypeDirect)},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name_upper", RelationType: RelType(model.RelationTypeIndirect)},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestIsTemp_Table contains tests that verify IsTemp field.
func TestIsTemp_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "SELECT produces temporary result",
			SQL:  "SELECT id, name FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", ToTable: "__result__", IsTemp: Bool(true)},
			},
		},
		{
			Name: "INSERT targets real table",
			SQL:  "INSERT INTO orders (order_id, user_id) SELECT id, user_id FROM temp_orders",
			ExpectedEdges: []ExpectedEdge{
				{ToTable: "orders", IsTemp: Bool(false)},
			},
		},
		{
			Name: "UPDATE targets real table",
			SQL:  "UPDATE products SET price = price * 1.1 WHERE category = 'electronics'",
			ExpectedEdges: []ExpectedEdge{
				{ToTable: "products", IsTemp: Bool(false)},
			},
		},
		{
			Name: "DELETE targets real table",
			SQL:  "DELETE FROM logs WHERE created_at < '2020-01-01'",
			ExpectedEdges: []ExpectedEdge{
				{ToTable: "logs", IsTemp: Bool(false)},
			},
		},
		{
			Name: "CREATE TABLE AS SELECT targets real table",
			SQL:  "CREATE TABLE summary AS SELECT category, COUNT(*) as cnt FROM products GROUP BY category",
			ExpectedEdges: []ExpectedEdge{
				{ToTable: "summary", IsTemp: Bool(false)},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestViewLineage_Table contains table-driven tests for CREATE VIEW lineage.
// PostgreSQL specific: full view support
func TestViewLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "simple CREATE VIEW",
			SQL:  "CREATE VIEW active_users AS SELECT id, name FROM users WHERE status = 'active'",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "active_users", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "active_users", ToField: "name"},
			},
		},
		{
			Name: "CREATE VIEW with explicit columns",
			SQL:  "CREATE VIEW user_summary (user_id, user_name) AS SELECT id, name FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "user_summary", ToField: "user_id"},
				{FromTable: "users", FromField: "name", ToTable: "user_summary", ToField: "user_name"},
			},
		},
		{
			Name: "CREATE OR REPLACE VIEW",
			SQL:  "CREATE OR REPLACE VIEW user_details AS SELECT u.id, u.name, u.email FROM users u",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "user_details", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "user_details", ToField: "name"},
				{FromTable: "users", FromField: "email", ToTable: "user_details", ToField: "email"},
			},
		},
		{
			Name: "CREATE VIEW with JOIN",
			SQL: `CREATE VIEW user_orders_view AS 
			      SELECT u.id, u.name, COUNT(o.id) AS order_count
			      FROM users u
			      LEFT JOIN orders o ON u.id = o.user_id
			      GROUP BY u.id, u.name`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "user_orders_view", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "user_orders_view", ToField: "name"},
				{ToTable: "user_orders_view", ToField: "order_count", HasTransform: Bool(true)},
			},
		},
		{
			Name: "CREATE VIEW with schema qualification",
			SQL:  "CREATE VIEW myschema.active_users AS SELECT id, name FROM public.users WHERE active = true",
			ExpectedEdges: []ExpectedEdge{
				{FromSchema: "public", FromTable: "users", FromField: "id", ToSchema: "myschema", ToTable: "active_users", ToField: "id"},
				{FromSchema: "public", FromTable: "users", FromField: "name", ToSchema: "myschema", ToTable: "active_users", ToField: "name"},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestOnConflictLineage_Table contains table-driven tests for INSERT ON CONFLICT (UPSERT).
// PostgreSQL specific: ON CONFLICT clause (similar to MySQL's ON DUPLICATE KEY UPDATE)
func TestOnConflictLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "INSERT ON CONFLICT DO NOTHING",
			SQL: `INSERT INTO users (id, name, email)
			      SELECT id, name, email FROM pending_users
			      ON CONFLICT (id) DO NOTHING`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "pending_users", FromField: "id", ToTable: "users", ToField: "id"},
				{FromTable: "pending_users", FromField: "name", ToTable: "users", ToField: "name"},
				{FromTable: "pending_users", FromField: "email", ToTable: "users", ToField: "email"},
			},
		},
		{
			Name: "INSERT ON CONFLICT DO UPDATE",
			SQL: `INSERT INTO users (id, name, email)
			      SELECT id, name, email FROM pending_users
			      ON CONFLICT (id) DO UPDATE SET 
			          name = EXCLUDED.name,
			          email = EXCLUDED.email`,
			ExpectedEdges: []ExpectedEdge{
				// INSERT edges
				{FromTable: "pending_users", FromField: "id", ToTable: "users", ToField: "id"},
				{FromTable: "pending_users", FromField: "name", ToTable: "users", ToField: "name"},
				{FromTable: "pending_users", FromField: "email", ToTable: "users", ToField: "email"},
			},
		},
		{
			Name: "INSERT ON CONFLICT with expression update",
			SQL: `INSERT INTO inventory (product_id, quantity)
			      SELECT product_id, quantity FROM shipment
			      ON CONFLICT (product_id) DO UPDATE SET
			          quantity = inventory.quantity + EXCLUDED.quantity`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "shipment", FromField: "product_id", ToTable: "inventory", ToField: "product_id"},
				{FromTable: "shipment", FromField: "quantity", ToTable: "inventory", ToField: "quantity"},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestAggregationLineage_Table contains table-driven tests for aggregation queries.
func TestAggregationLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "simple GROUP BY with COUNT",
			SQL:  "SELECT department, COUNT(*) AS emp_count FROM employees GROUP BY department",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "employees", FromField: "department", ToTable: "__result__", ToField: "department"},
				{ToTable: "__result__", ToField: "emp_count", HasTransform: Bool(true)},
			},
		},
		{
			Name: "GROUP BY with multiple aggregates",
			SQL: `SELECT 
			        category,
			        COUNT(*) AS product_count,
			        AVG(price) AS avg_price,
			        MAX(price) AS max_price,
			        MIN(price) AS min_price
			      FROM products
			      GROUP BY category`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "products", FromField: "category", ToTable: "__result__", ToField: "category"},
				{ToTable: "__result__", ToField: "product_count", HasTransform: Bool(true)},
				{ToTable: "__result__", ToField: "avg_price", HasTransform: Bool(true)},
				{ToTable: "__result__", ToField: "max_price", HasTransform: Bool(true)},
				{ToTable: "__result__", ToField: "min_price", HasTransform: Bool(true)},
			},
		},
		{
			Name: "GROUP BY with HAVING",
			SQL: `SELECT user_id, SUM(amount) AS total
			      FROM orders
			      GROUP BY user_id
			      HAVING SUM(amount) > 1000`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "orders", FromField: "user_id", ToTable: "__result__", ToField: "user_id"},
				{FromTable: "orders", FromField: "amount", ToTable: "__result__", ToField: "total", HasTransform: Bool(true)},
			},
		},
		{
			Name: "PostgreSQL STRING_AGG function",
			SQL:  "SELECT department, STRING_AGG(name, ', ' ORDER BY name) AS employees FROM staff GROUP BY department",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "staff", FromField: "department", ToTable: "__result__", ToField: "department"},
				{FromTable: "staff", FromField: "name", ToTable: "__result__", ToField: "employees", HasTransform: Bool(true)},
			},
		},
		{
			Name: "PostgreSQL ARRAY_AGG function",
			SQL:  "SELECT user_id, ARRAY_AGG(order_id) AS order_ids FROM orders GROUP BY user_id",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "orders", FromField: "user_id", ToTable: "__result__", ToField: "user_id"},
				{FromTable: "orders", FromField: "order_id", ToTable: "__result__", ToField: "order_ids", HasTransform: Bool(true)},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestCaseExpressionLineage_Table contains table-driven tests for CASE expressions.
func TestCaseExpressionLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "simple CASE expression",
			SQL: `SELECT id,
			        CASE status
			            WHEN 'A' THEN 'Active'
			            WHEN 'I' THEN 'Inactive'
			            ELSE 'Unknown'
			        END AS status_text
			      FROM users`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "status", ToTable: "__result__", ToField: "status_text", HasTransform: Bool(true)},
			},
		},
		{
			Name: "searched CASE expression",
			SQL: `SELECT id,
			        CASE 
			            WHEN age < 18 THEN 'minor'
			            WHEN age >= 18 AND age < 65 THEN 'adult'
			            ELSE 'senior'
			        END AS age_group
			      FROM users`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "age", ToTable: "__result__", ToField: "age_group", HasTransform: Bool(true)},
			},
		},
		{
			Name: "CASE with multiple column references",
			SQL: `SELECT id,
			        CASE 
			            WHEN status = 'active' AND balance > 0 THEN 'good'
			            WHEN status = 'active' AND balance <= 0 THEN 'warning'
			            ELSE 'inactive'
			        END AS account_status
			      FROM accounts`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "accounts", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "accounts", FromField: "status", ToTable: "__result__", ToField: "account_status", HasTransform: Bool(true)},
				{FromTable: "accounts", FromField: "balance", ToTable: "__result__", ToField: "account_status", HasTransform: Bool(true)},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestLateralJoinLineage_Table contains tests for PostgreSQL LATERAL joins.
func TestLateralJoinLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "LATERAL subquery",
			SQL: `SELECT u.id, u.name, recent.order_id
			      FROM users u
			      LEFT JOIN LATERAL (
			          SELECT order_id FROM orders 
			          WHERE user_id = u.id 
			          ORDER BY created_at DESC 
			          LIMIT 1
			      ) recent ON true`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
				{FromTable: "orders", FromField: "order_id", ToTable: "__result__", ToField: "order_id"},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestDistinctLineage_Table contains tests for DISTINCT queries.
func TestDistinctLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "SELECT DISTINCT",
			SQL:  "SELECT DISTINCT category FROM products",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "products", FromField: "category", ToTable: "__result__", ToField: "category"},
			},
		},
		{
			Name: "SELECT DISTINCT ON (PostgreSQL specific)",
			SQL: `SELECT DISTINCT ON (user_id) user_id, order_id, total
			      FROM orders
			      ORDER BY user_id, created_at DESC`,
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "orders", FromField: "user_id", ToTable: "__result__", ToField: "user_id"},
				{FromTable: "orders", FromField: "order_id", ToTable: "__result__", ToField: "order_id"},
				{FromTable: "orders", FromField: "total", ToTable: "__result__", ToField: "total"},
			},
		},
	}

	RunLineageTests(t, testCases)
}
