-- The output budget moved from the provider profile to each model entry. Copy a
-- profile's stored budget onto every model that does not already carry one, then
-- drop the retired profile key. protojson emits camelCase keys and omits zero
-- values, so a profile without the key used the server default and nothing is
-- copied. Idempotent: the guard and the per-model check make a second run a
-- no-op. A models value that is not an array (null/object from a hand-edited
-- row) is left alone except for the retired key, so one malformed row cannot
-- fail the whole startup migration.
UPDATE llm_provider_profile
SET metadata =
  CASE
    WHEN jsonb_typeof(metadata -> 'models') = 'array' THEN
      jsonb_set(
        metadata - 'maxOutputTokens',
        '{models}',
        COALESCE(
          (
            SELECT jsonb_agg(
                     CASE
                       WHEN model ? 'maxOutputTokens' THEN model
                       ELSE model || jsonb_build_object('maxOutputTokens', metadata -> 'maxOutputTokens')
                     END
                     ORDER BY ordinality)
            FROM jsonb_array_elements(metadata -> 'models')
                 WITH ORDINALITY AS entries(model, ordinality)
          ),
          '[]'::jsonb)
      )
    ELSE metadata - 'maxOutputTokens'
  END
WHERE metadata ? 'maxOutputTokens';
