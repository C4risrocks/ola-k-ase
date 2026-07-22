CREATE TABLE IF NOT EXISTS profile (
	name TEXT,
	title TEXT,
	email TEXT,
	phone TEXT,
	github TEXT,
	nationality TEXT,
	about_text TEXT
);

CREATE TABLE IF NOT EXISTS skills (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	category TEXT,
	name TEXT,
	items TEXT
);

CREATE TABLE IF NOT EXISTS experience (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT,
	organization TEXT,
	description TEXT,
	date_range TEXT,
	details TEXT
);

CREATE TABLE IF NOT EXISTS education (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	institution TEXT,
	degree TEXT,
	date_range TEXT
);

CREATE TABLE IF NOT EXISTS courses (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT,
	institution TEXT,
	date_range TEXT
);

CREATE TABLE IF NOT EXISTS contact_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT,
	email TEXT,
	message TEXT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
