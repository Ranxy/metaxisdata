-- Segment fixture, step 1: lose the 'east' segment.
-- The first segment_check run recorded the baseline; this run must FAIL with one
-- disappeared segment.

USE e2e_dq;

DELETE FROM dq_segment WHERE region = 'east';
