-- A run no longer shows how many items it reported or their names: without a
-- record of earlier runs, calling them new told the reader nothing true. The
-- run's status still says whether it had anything to report.

ALTER TABLE schedule_runs DROP COLUMN labels;
ALTER TABLE schedule_runs DROP COLUMN new_count;
