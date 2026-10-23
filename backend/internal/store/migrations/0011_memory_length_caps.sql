UPDATE user_memory
SET content = substr(content, 1, 3000)
WHERE length(content) > 3000;

UPDATE project_memory
SET content = substr(content, 1, 3000)
WHERE length(content) > 3000;
