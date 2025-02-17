CREATE TABLE IF NOT EXISTS areas(
  id INT NOT NULL,
  name VARCHAR(255) NOT NULL,
  description VARCHAR(255),
  is_custom BOOL NOT NULL DEFAULT false,
  contact_name VARCHAR(255),
  area_json JSON,
  created_at TIMESTAMP,
  updated_at TIMESTAMP,
  PRIMARY KEY (id)
 );

CREATE TABLE IF NOT EXISTS contact_emails(
  email CHAR(64) NOT NULL,
  consent_report BOOL NOT NULL DEFAULT true,
  PRIMARY KEY (email)
);

CREATE TABLE IF NOT EXISTS area_index(
  area_id INT NOT NULL,
  polygon_s2cell BIGINT,
  INDEX polygon_idx (polygon_s2cell)
);
