-- The repair: every seeded defect fixed, so a rerun turns FAIL into PASS and
-- each open issue into RESOLVED(AUTO_RECOVERED).
--
--   dq_orders   NULL emails, duplicate refs, negative amount, unaccepted status,
--               short code, orphan customer, shipped > quantity, bad USCC
--   dq_groups   bucket 'a' back under the allowed size
--   dq_heartbeat  fresh timestamp
--   dq_segment  the lost 'east' segment comes back (81_segment_restore_region.sql
--               is separate because segment_check needs one more run first)

USE e2e_dq;

UPDATE dq_orders SET customer_email = 'bob@example.com'   WHERE id = 2;
UPDATE dq_orders SET customer_email = 'bob2@example.com'  WHERE id = 3;
UPDATE dq_orders SET external_ref   = 'R-6'               WHERE id = 6;
UPDATE dq_orders SET amount         = 5.00                WHERE id = 4;
UPDATE dq_orders SET status         = 'paid'              WHERE id = 5;
UPDATE dq_orders SET product_code   = 'XYZ'               WHERE id = 4;
UPDATE dq_orders SET customer_id    = 1                   WHERE id = 7;
UPDATE dq_orders SET shipped_quantity = quantity          WHERE id = 6;
UPDATE dq_orders SET credit_code    = '91350100M000100Y43' WHERE id = 10;
-- id 10 shares (customer_id, product_code) with id 1; move it to its own code.
UPDATE dq_orders SET product_code   = 'QQQ'               WHERE id = 10;

DELETE FROM dq_groups WHERE id = 3;

UPDATE dq_heartbeat SET updated_at = NOW();
