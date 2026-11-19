-- An artifact carries the project its thread was in when it was made, and the
-- projects FK cascades it. A thread moved on before moves carried their
-- artifacts left them naming the old project: deleting that project took a
-- live thread's artifacts along. Align them with their thread's project, as
-- UpdateThread now does on a move. One backing a project's knowledge is that
-- project's and stays; one without a project (a composer upload) follows its
-- thread already.
UPDATE artifacts
SET project_id = (SELECT t.project_id FROM threads t
                  WHERE t.user_id = artifacts.user_id AND t.id = artifacts.thread_id)
WHERE thread_id IS NOT NULL
  AND project_id IS NOT NULL
  AND EXISTS (SELECT 1 FROM threads t
              WHERE t.user_id = artifacts.user_id AND t.id = artifacts.thread_id
                AND t.project_id IS NOT artifacts.project_id)
  AND NOT EXISTS (SELECT 1 FROM documents d
                  WHERE d.user_id = artifacts.user_id AND d.artifact_id = artifacts.id
                    AND d.project_id IS NOT NULL);
