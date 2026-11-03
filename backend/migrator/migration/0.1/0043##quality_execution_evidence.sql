-- The sample column now carries the whole evidence of one execution: failed
-- rows, passing rows for the good-vs-bad comparison, and the segment set a
-- segment check judges its next run against. The payload shape changed, the
-- column does not.
COMMENT ON COLUMN quality_execution.sample IS 'Stored as QualityExecutionEvidence (proto/store/store/data_quality.proto)';
