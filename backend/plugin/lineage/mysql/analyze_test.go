package mysql

import (
	"testing"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// TestSelectLineage_Table contains table-driven tests for SELECT statement lineage.
// This demonstrates the refactored test approach using the unified test framework.
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
			Name: "select with expression (CONCAT)",
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
			Name: "select with parenthesized join tree",
			SQL:  "SELECT `o`.`id` AS `order_id`,`c`.`name` AS `customer_name`,sum(`oi`.`amount`) AS `total_amount` FROM (((`orders` `o` JOIN `customers` `c` ON((`o`.`customer_id` = `c`.`id`)))) LEFT JOIN `order_items` `oi` ON((`o`.`id` = `oi`.`order_id`))) GROUP BY `o`.`id`,`c`.`name`",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "orders", FromField: "id", ToTable: "__result__", ToField: "order_id", IsTemp: Bool(true)},
				{FromTable: "customers", FromField: "name", ToTable: "__result__", ToField: "customer_name", IsTemp: Bool(true)},
				{FromTable: "order_items", FromField: "amount", ToTable: "__result__", ToField: "total_amount", HasTransform: Bool(true), IsTemp: Bool(true)},
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
			Name: "INSERT SELECT with parenthesized join tree",
			SQL:  "INSERT INTO order_summary (`order_id`, `customer_name`, `total_amount`) SELECT `o`.`id`,`c`.`name`,sum(`oi`.`amount`) AS `total_amount` FROM (((`orders` `o` JOIN `customers` `c` ON((`o`.`customer_id` = `c`.`id`)))) LEFT JOIN `order_items` `oi` ON((`o`.`id` = `oi`.`order_id`))) GROUP BY `o`.`id`,`c`.`name`",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "orders", FromField: "id", ToTable: "order_summary", ToField: "order_id", IsTemp: Bool(false)},
				{FromTable: "customers", FromField: "name", ToTable: "order_summary", ToField: "customer_name", IsTemp: Bool(false)},
				{FromTable: "order_items", FromField: "amount", ToTable: "order_summary", ToField: "total_amount", HasTransform: Bool(true), IsTemp: Bool(false)},
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
			Name: "CREATE TEMPORARY TABLE AS SELECT",
			SQL:  "CREATE TEMPORARY TABLE tmp_active_users AS SELECT id, name FROM users WHERE status = 'active'",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "tmp_active_users", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "tmp_active_users", ToField: "name"},
			},
		},
		{
			Name: "CREATE TABLE AS SELECT with parenthesized join tree",
			SQL:  "CREATE TABLE order_rollup AS SELECT `o`.`id` AS `order_id`,`c`.`name` AS `customer_name`,sum(`oi`.`amount`) AS `total_amount` FROM (((`orders` `o` JOIN `customers` `c` ON((`o`.`customer_id` = `c`.`id`)))) LEFT JOIN `order_items` `oi` ON((`o`.`id` = `oi`.`order_id`))) GROUP BY `o`.`id`,`c`.`name`",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "orders", FromField: "id", ToTable: "order_rollup", ToField: "order_id", IsTemp: Bool(false)},
				{FromTable: "customers", FromField: "name", ToTable: "order_rollup", ToField: "customer_name", IsTemp: Bool(false)},
				{FromTable: "order_items", FromField: "amount", ToTable: "order_rollup", ToField: "total_amount", HasTransform: Bool(true), IsTemp: Bool(false)},
			},
		},
	}

	RunLineageTests(t, testCases)
}

// TestCreateViewLineage_Table contains table-driven tests for CREATE VIEW AS SELECT.
func TestCreateViewLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "CREATE VIEW with quoted identifiers",
			SQL:  "CREATE VIEW `ods_temp_view1` AS SELECT `src_users`.`id` AS `user_id`, `src_users`.`name` FROM `src_users`",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "src_users", FromField: "id", ToTable: "ods_temp_view1", ToField: "user_id"},
				{FromTable: "src_users", FromField: "name", ToTable: "ods_temp_view1", ToField: "name"},
			},
		},
		{
			Name: "CREATE VIEW with parenthesized join tree",
			SQL:  "CREATE VIEW `popular_posts` AS select `p`.`id` AS `id`,`p`.`user_id` AS `user_id`,`p`.`title` AS `title`,`p`.`content` AS `content`,`p`.`status` AS `status`,`p`.`created_at` AS `created_at`,`p`.`published_at` AS `published_at`,`p`.`view_count` AS `view_count`,`p`.`likes` AS `likes`,`p`.`metadata` AS `metadata`,`u`.`username` AS `username`,count(`c`.`id`) AS `comment_count` from ((`posts` `p` join `users` `u` on((`p`.`user_id` = `u`.`id`))) left join `comments` `c` on((`p`.`id` = `c`.`post_id`))) where (`p`.`status` = 'published') group by `p`.`id` having (`comment_count` > 0) order by `p`.`likes` desc,`comment_count` desc",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "posts", FromField: "id", ToTable: "popular_posts", ToField: "id", IsTemp: Bool(false)},
				{FromTable: "posts", FromField: "user_id", ToTable: "popular_posts", ToField: "user_id", IsTemp: Bool(false)},
				{FromTable: "posts", FromField: "title", ToTable: "popular_posts", ToField: "title", IsTemp: Bool(false)},
				{FromTable: "posts", FromField: "content", ToTable: "popular_posts", ToField: "content", IsTemp: Bool(false)},
				{FromTable: "posts", FromField: "status", ToTable: "popular_posts", ToField: "status", IsTemp: Bool(false)},
				{FromTable: "posts", FromField: "created_at", ToTable: "popular_posts", ToField: "created_at", IsTemp: Bool(false)},
				{FromTable: "posts", FromField: "published_at", ToTable: "popular_posts", ToField: "published_at", IsTemp: Bool(false)},
				{FromTable: "posts", FromField: "view_count", ToTable: "popular_posts", ToField: "view_count", IsTemp: Bool(false)},
				{FromTable: "posts", FromField: "likes", ToTable: "popular_posts", ToField: "likes", IsTemp: Bool(false)},
				{FromTable: "posts", FromField: "metadata", ToTable: "popular_posts", ToField: "metadata", IsTemp: Bool(false)},
				{FromTable: "users", FromField: "username", ToTable: "popular_posts", ToField: "username", IsTemp: Bool(false)},
				{FromTable: "comments", FromField: "id", ToTable: "popular_posts", ToField: "comment_count", HasTransform: Bool(true), IsTemp: Bool(false)},
			},
		},
		{
			Name: "CREATE VIEW with deeply nested parenthesized join tree",
			SQL:  "CREATE VIEW `order_rollup` AS select `o`.`id` AS `order_id`,`c`.`name` AS `customer_name`,sum(`oi`.`amount`) AS `total_amount` from (((`orders` `o` join `customers` `c` on((`o`.`customer_id` = `c`.`id`)))) left join `order_items` `oi` on((`o`.`id` = `oi`.`order_id`))) group by `o`.`id`,`c`.`name`",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "orders", FromField: "id", ToTable: "order_rollup", ToField: "order_id", IsTemp: Bool(false)},
				{FromTable: "customers", FromField: "name", ToTable: "order_rollup", ToField: "customer_name", IsTemp: Bool(false)},
				{FromTable: "order_items", FromField: "amount", ToTable: "order_rollup", ToField: "total_amount", HasTransform: Bool(true), IsTemp: Bool(false)},
			},
		},
		{
			Name: "CREATE VIEW with parenthesized right join",
			SQL:  "CREATE VIEW `shipment_status_view` AS select `o`.`id` AS `order_id`,`s`.`shipped_at` AS `shipped_at`,`s`.`status` AS `shipping_status` from ((`orders` `o` right join `shipments` `s` on((`o`.`id` = `s`.`order_id`)))) where (`s`.`status` <> 'cancelled')",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "orders", FromField: "id", ToTable: "shipment_status_view", ToField: "order_id", IsTemp: Bool(false)},
				{FromTable: "shipments", FromField: "shipped_at", ToTable: "shipment_status_view", ToField: "shipped_at", IsTemp: Bool(false)},
				{FromTable: "shipments", FromField: "status", ToTable: "shipment_status_view", ToField: "shipping_status", IsTemp: Bool(false)},
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
			SQL:  "UPDATE users SET new_email = old_email, full_name = CONCAT(first_name, ' ', last_name)",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "old_email", ToTable: "users", ToField: "new_email"},
				{FromTable: "users", FromField: "first_name", ToTable: "users", ToField: "full_name", HasTransform: Bool(true)},
				{FromTable: "users", FromField: "last_name", ToTable: "users", ToField: "full_name", HasTransform: Bool(true)},
			},
		},
		{
			Name: "UPDATE with JOIN",
			SQL: `UPDATE orders o 
			      JOIN users u ON o.user_id = u.id 
			      SET o.user_name = u.name, o.discount = o.total * 0.1`,
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
	}

	RunLineageTests(t, testCases)
}

// TestDeleteLineage_Table contains table-driven tests for DELETE statement lineage.
func TestDeleteLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "simple DELETE",
			SQL:  "DELETE FROM users WHERE status = 'inactive' AND last_login < DATE_SUB(NOW(), INTERVAL 1 YEAR)",
			ExpectedEdges: []ExpectedEdge{
				{FromField: "status", ToTable: "users", ToField: "__deletion__", HasTransform: Bool(true)},
				{FromField: "last_login", ToTable: "users", ToField: "__deletion__", HasTransform: Bool(true)},
			},
		},
		{
			Name: "DELETE with JOIN",
			SQL: `DELETE o FROM orders o
			      JOIN users u ON o.user_id = u.id
			      WHERE u.status = 'deleted'`,
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
	}

	RunLineageTests(t, testCases)
}

// TestReplaceLineage_Table contains table-driven tests for REPLACE statement lineage.
func TestReplaceLineage_Table(t *testing.T) {
	testCases := []LineageTestCase{
		{
			Name: "REPLACE SELECT",
			SQL:  "REPLACE INTO tmp_users SELECT id, name FROM users WHERE age > 30",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "tmp_users", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "tmp_users", ToField: "name"},
			},
		},
		{
			Name: "REPLACE SELECT with explicit columns",
			SQL:  "REPLACE INTO tmp_users (user_id, user_name) SELECT id, name FROM users",
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "tmp_users", ToField: "user_id"},
				{FromTable: "users", FromField: "name", ToTable: "tmp_users", ToField: "user_name"},
			},
		},
		{
			Name: "REPLACE SELECT with JOIN",
			SQL: `REPLACE INTO summary 
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
			ExpectedEdges: []ExpectedEdge{
				{FromTable: "users", FromField: "id", ToTable: "__result__", ToField: "id"},
				{FromTable: "users", FromField: "name", ToTable: "__result__", ToField: "name"},
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
