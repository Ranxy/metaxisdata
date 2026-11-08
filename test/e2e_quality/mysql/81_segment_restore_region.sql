-- Segment fixture, step 2: bring 'east' back so the next run passes and the open
-- issue recovers.

USE e2e_dq;

INSERT INTO dq_segment (id, region) VALUES (4, 'east');
