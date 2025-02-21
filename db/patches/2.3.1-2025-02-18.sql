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
  area_id INT NOT NULL,
  email CHAR(64) NOT NULL,
  consent_report BOOL NOT NULL DEFAULT true,
  INDEX area_id_index (area_id),
  INDEX email_index (email)
);

CREATE TABLE IF NOT EXISTS area_index(
  area_id INT NOT NULL,
  geom GEOMETRY NOT NULL SRID 4326,
  SPATIAL INDEX(geom)
);
