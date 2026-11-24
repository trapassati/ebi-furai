-- Articles without a title were stored as "(untitled)" for everyone; new
-- ones get it in the language of whoever follows the feed.
UPDATE articles SET title = '(senza titolo)'
WHERE title = '(untitled)' AND user_id IN (SELECT id FROM users WHERE lang = 'it');
