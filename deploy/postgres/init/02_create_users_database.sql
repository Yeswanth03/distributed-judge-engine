-- Local development only: the User Service's own login, owning only its own database.
CREATE USER users WITH PASSWORD 'users';
CREATE DATABASE users OWNER users;
