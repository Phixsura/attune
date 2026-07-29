-- 122: Persist last connectivity test result on cohort sources (#233).
ALTER TABLE cohort_sources
  ADD COLUMN last_tested_at  timestamptz,
  ADD COLUMN last_test_ok    boolean;
