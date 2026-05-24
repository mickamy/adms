CREATE TABLE users (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    email VARCHAR(255) NOT NULL UNIQUE,
    name VARCHAR(255) NOT NULL,
    role VARCHAR(32) NOT NULL DEFAULT 'member',
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE posts (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    title VARCHAR(255) NOT NULL,
    body TEXT,
    tags JSON,
    view_count INT NOT NULL DEFAULT 0,
    published BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX posts_user_id_idx (user_id),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE comments (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    post_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    body TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    INDEX comments_post_id_idx (post_id),
    INDEX comments_user_id_idx (user_id),
    FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

INSERT INTO users (email, name, role, active, created_at) VALUES
    ('alice@example.com',  'Alice Anderson', 'admin',  TRUE,  '2025-01-15 09:00:00'),
    ('bob@example.com',    'Bob Baker',      'member', TRUE,  '2025-02-03 10:30:00'),
    ('carol@example.com',  'Carol Carter',   'member', TRUE,  '2025-03-20 14:00:00'),
    ('dan@example.com',    'Dan Davis',      'member', FALSE, '2025-04-05 08:15:00'),
    ('eve@example.com',    'Eve Evans',      'editor', TRUE,  '2025-05-10 11:45:00'),
    ('frank@example.com',  'Frank Foster',   'member', TRUE,  '2025-06-22 16:20:00'),
    ('grace@example.com',  'Grace Garcia',   'editor', TRUE,  '2025-07-08 13:10:00');

INSERT INTO posts (user_id, title, body, tags, view_count, published, created_at) VALUES
    (1, 'Welcome to adms',     'This is the first post on our showcase.',     '["welcome", "intro"]',    42,  TRUE,  '2025-01-16 10:00:00'),
    (1, 'How to query',         'Use PostgREST-style operators in the URL.',   '["docs", "tutorial"]',    87,  TRUE,  '2025-01-20 15:30:00'),
    (2, 'My first post',        'Just trying things out.',                     NULL,                       12,  TRUE,  '2025-02-05 09:00:00'),
    (2, 'Draft thoughts',       'Work in progress, not ready yet.',            NULL,                        0,  FALSE, '2025-02-10 21:00:00'),
    (3, 'Carol on adms',        'I really like this tool so far.',             '["review"]',              23,  TRUE,  '2025-03-25 12:00:00'),
    (5, 'Editor pick',          'Best of the week.',                            '["editor", "pick"]',     156, TRUE,  '2025-05-15 08:30:00'),
    (5, 'Coming soon',          'Stay tuned for more.',                         NULL,                        5,  FALSE, '2025-05-20 17:00:00'),
    (1, 'Performance tips',     'Indexing and EXPLAIN ANALYZE basics.',        '["perf", "docs"]',        64,  TRUE,  '2025-06-01 13:00:00'),
    (3, 'Quick update',         'Short post, nothing to see here.',            '["update"]',               8,  TRUE,  '2025-06-10 19:30:00'),
    (2, 'Bob returns',          'I am back from vacation!',                     NULL,                       19,  TRUE,  '2025-07-04 11:15:00'),
    (6, 'New member intro',     'Hi everyone, glad to be here.',               '["intro"]',                3,  TRUE,  '2025-07-22 09:45:00'),
    (7, 'Editorial guidelines', 'Please follow these when posting.',           '["docs", "editor"]',      77,  TRUE,  '2025-08-09 14:00:00');

INSERT INTO comments (post_id, user_id, body, created_at) VALUES
    (1,  2, 'Great intro!',                       '2025-01-16 11:00:00'),
    (1,  3, 'Welcome aboard.',                    '2025-01-16 12:00:00'),
    (1,  5, 'Looking forward to more.',           '2025-01-17 09:00:00'),
    (2,  2, 'This helped me a lot.',              '2025-01-21 10:00:00'),
    (2,  3, 'I had the same question.',           '2025-01-21 14:30:00'),
    (2,  4, 'Any plans for filter docs?',         '2025-01-22 08:00:00'),
    (3,  1, 'Welcome Bob!',                       '2025-02-05 10:00:00'),
    (5,  1, 'Glad you like it.',                  '2025-03-25 13:00:00'),
    (5,  5, 'Same here.',                         '2025-03-26 09:00:00'),
    (6,  1, 'Thanks for the pick.',               '2025-05-15 10:00:00'),
    (6,  2, 'Bookmarked.',                        '2025-05-15 14:00:00'),
    (6,  3, 'Nice list.',                         '2025-05-16 08:00:00'),
    (8,  2, 'Will try EXPLAIN ANALYZE.',          '2025-06-01 15:00:00'),
    (8,  5, 'Add link to docs?',                  '2025-06-02 09:00:00'),
    (9,  1, 'Got it.',                            '2025-06-10 20:00:00'),
    (10, 1, 'Welcome back!',                      '2025-07-04 12:00:00'),
    (10, 3, 'Missed you Bob.',                    '2025-07-04 13:00:00'),
    (10, 5, 'Cheers.',                            '2025-07-05 08:00:00'),
    (11, 1, 'Hi Frank.',                          '2025-07-22 10:30:00'),
    (11, 7, 'Welcome.',                           '2025-07-22 11:00:00'),
    (12, 1, 'Pinned for reference.',              '2025-08-09 15:00:00'),
    (12, 5, 'Useful.',                            '2025-08-10 09:30:00');
