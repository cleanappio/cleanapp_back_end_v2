
CREATE TABLE actions(
  id CHAR(32) NOT NULL,
  name VARCHAR(255) NOT NULL,
  is_active INT NOT NULL DEFAULT 0,
  expiration_date DATE,
  PRIMARY KEY (id)
);

ALTER TABLE users ADD action_id CHAR(32);
ALTER TABLE users ADD INDEX action_idx (action_id);

ALTER TABLE users_shadow ADD action_id CHAR(32);
ALTER TABLE users_shadow ADD INDEX action_idx (action_id);

ALTER TABLE reports ADD action_id CHAR(32);
ALTER TABLE reports ADD INDEX action_idx (action_id);