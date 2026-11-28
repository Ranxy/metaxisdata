-- An ask now carries the structured decision it puts to the owner: question ids,
-- optional choices and the selected labels the answer echoes. The text column stays
-- as the summary for notifications and as the fallback for legacy asks.
ALTER TABLE quality_agent_ask
    ADD COLUMN IF NOT EXISTS questions JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS answers JSONB NOT NULL DEFAULT '{}';
COMMENT ON COLUMN quality_agent_ask.questions IS 'Stored as QualityAgentAskQuestionList (proto/store/store/data_quality.proto); empty on a legacy ask';
COMMENT ON COLUMN quality_agent_ask.answers IS 'Stored as QualityAgentAskAnswerList (proto/store/store/data_quality.proto); empty until the owner answers';
