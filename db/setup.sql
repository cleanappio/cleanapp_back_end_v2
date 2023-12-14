-- Initial database setup. Run as root@
-- Starts with setting passwords from the environment.

-- Generate init1.sql from init.sql with right passwords.
system sh -e ./set_passwords.sh

source tmp/init1.sql
