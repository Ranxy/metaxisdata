-- A draft's proposing agent may retract it before a person decides. WITHDRAWN is
-- that closure: not a reviewer's decision, and the same identity may be proposed
-- again later. The status is an int2, so only the catalog comment changes.
COMMENT ON COLUMN quality_suggestion.status IS '1 PENDING, 2 ACCEPTED, 3 REJECTED, 4 SUPERSEDED, 5 WITHDRAWN (retracted by its proposing agent)';
