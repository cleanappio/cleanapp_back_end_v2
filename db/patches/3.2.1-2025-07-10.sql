CREATE TABLE IF NOT EXISTS reports_geometry(
  seq INT NOT NULL,
  geom GEOMETRY NOT NULL SRID 4326,
  PRIMARY KEY (seq),
  SPATIAL INDEX(geom)
);

INSERT INTO reports_geometry
  SELECT seq, ST_SRID(POINT(longitude, latitude), 4326)
  FROM reports
  WHERE seq NOT IN(
    SELECT seq FROM reports_geometry
  );
