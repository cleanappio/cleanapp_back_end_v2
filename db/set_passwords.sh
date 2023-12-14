# Update passwords in the SQL initialization file.

cat init.sql | \
  sed "s|secret_app|$MYSQL_APP_PASSWORD|g;s|secret_importer|$MYSQL_READER_PASSWORD|g" \
  > tmp/init1.sql
