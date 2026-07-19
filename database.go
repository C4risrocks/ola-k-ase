package main

import (
	"database/sql"
	"encoding/json"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

type Profile struct {
	Name        string
	Title       string
	Email       string
	Phone       string
	Github      string
	Nationality string
	AboutText   string
}

type Skill struct {
	ID       int
	Category string
	Name     string
	Items    string
}

type Experience struct {
	ID           int
	Title        string
	Organization string
	Description  string
	DateRange    string
	Details      []string
}

type Education struct {
	ID          int
	Institution string
	Degree      string
	DateRange   string
}

type Course struct {
	ID          int
	Name        string
	Institution string
	DateRange   string
}

type ContactMessage struct {
	ID        int
	Name      string
	Email     string
	Message   string
	CreatedAt string
}

func initDB(dataSourceName string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, err
	}

	createTables := `
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
	`

	_, err = db.Exec(createTables)
	if err != nil {
		return nil, err
	}

	seedData(db)

	return db, nil
}

func seedData(db *sql.DB) {
	var count int
	db.QueryRow("SELECT COUNT(*) FROM profile").Scan(&count)
	if count > 0 {
		return // Data already seeded
	}

	tx, err := db.Begin()
	if err != nil {
		log.Println("Error starting transaction:", err)
		return
	}

	_, err = tx.Exec(`INSERT INTO profile (name, title, email, phone, github, nationality, about_text) 
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"Christian de Jesús Moreno Marín",
		"Computer Engineering Student",
		"c4risrocks@gmail.com",
		"5514938997",
		"www.github.com/C4risrocks",
		"Mexican",
		"Technology Enthusiast usually likes to play videogames, pizza designer with passion for coding, music lover since forever. Always with the hunger of acquire knowledge through investigation and research. Crazy about Gadgets.")
	if err != nil {
		tx.Rollback()
		return
	}

	skills := []Skill{
		{Category: "Switching", Name: "Switching", Items: "IEEE 802.3, IEEE 802.1q, STP, RSTP, PVST, VLANs, Etherchannel, Collision Domains, Broadcast Domains"},
		{Category: "Routing", Name: "Routing", Items: "RIPv1, RIPv2, RIPng, OSPFv2, OSPFv3, FHRP (HSRP), EIGRP, GRE tunnel"},
		{Category: "Security", Name: "Security", Items: "AAA model, Radius Server, Cisco Privilege Levels, View based roles in Cisco devices"},
		{Category: "WAN", Name: "WAN", Items: "Frame Relay (Point to point, Hub and Spoke, Full Mesh)"},
		{Category: "Programming", Name: "Programming", Items: "C, C++, Python, JavaScript/Node.js, Java, Oracle SQL, MySQL"},
		{Category: "Hardware Description", Name: "Hardware Description", Items: "VHDL"},
		{Category: "Operating Systems", Name: "Operating Systems", Items: "Linux (Debian, Ubuntu, Manjaro, CentOS), Windows, macOS, UNIX based"},
		{Category: "Software & Tools", Name: "Software & Tools", Items: "Excel, Word, PowerPoint, Docker, CISCO CML (VIRL), Oracle SQL Data Modeler, Oracle SQL Data Developer, Apache/Tomcat, Bash/sh/PowerShell/zsh"},
	}

	for _, s := range skills {
		_, err = tx.Exec("INSERT INTO skills (category, name, items) VALUES (?, ?, ?)", s.Category, s.Name, s.Items)
		if err != nil {
			tx.Rollback()
			return
		}
	}

	detailsJSON, _ := json.Marshal([]string{
		"Web Development for telecomfi.unam.mx",
		"Database support (Oracle Database 11g, Oracle SQL Developer)",
		"Maintenance of Cisco devices at Cisco Interconnectivity Laboratory",
		"Helping Instructors with CCNA and Security Network labs",
		"GRE tunnel implementations (IPv4/IPv6)",
		"AAA Authentication/Authorization (Local and Radius Server)",
		"Radius Server with freeradius and Windows Server (Active Directory)",
	})

	_, err = tx.Exec(`INSERT INTO experience (title, organization, description, date_range, details) 
		VALUES (?, ?, ?, ?, ?)`,
		"Social Service",
		"Telecommunications Department, Facultad de Ingeniería, UNAM",
		"Development, Networking and Documentation",
		"February 2019 – Present",
		string(detailsJSON))
	if err != nil {
		tx.Rollback()
		return
	}

	_, err = tx.Exec(`INSERT INTO education (institution, degree, date_range) VALUES (?, ?, ?)`,
		"Facultad de Ingeniería, Universidad Nacional Autónoma de México",
		"Computer Engineering",
		"")
	if err != nil {
		tx.Rollback()
		return
	}

	courses := []Course{
		{Name: "Hardware Design in VHDL for FPGAs", Institution: "INTESC COURSES", DateRange: "Jan 2019 – Feb 2019"},
		{Name: "CCNA Enterprise", Institution: "Facultad de Ingeniería, UNAM", DateRange: "Aug 2019 – Dec 2019"},
		{Name: "CCNP Security", Institution: "Facultad de Ingeniería, UNAM", DateRange: "Feb 2020 – Present"},
		{Name: "CCNA Enterprise", Institution: "Virtual Education Community Program, Cisco", DateRange: "Mar 2020 – Present"},
	}

	for _, c := range courses {
		_, err = tx.Exec("INSERT INTO courses (name, institution, date_range) VALUES (?, ?, ?)", c.Name, c.Institution, c.DateRange)
		if err != nil {
			tx.Rollback()
			return
		}
	}

	tx.Commit()
}

func getProfile(db *sql.DB) (Profile, error) {
	var p Profile
	err := db.QueryRow("SELECT name, title, email, phone, github, nationality, about_text FROM profile LIMIT 1").
		Scan(&p.Name, &p.Title, &p.Email, &p.Phone, &p.Github, &p.Nationality, &p.AboutText)
	return p, err
}

func getSkills(db *sql.DB) ([]Skill, error) {
	rows, err := db.Query("SELECT id, category, name, items FROM skills")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var skills []Skill
	for rows.Next() {
		var s Skill
		if err := rows.Scan(&s.ID, &s.Category, &s.Name, &s.Items); err != nil {
			return nil, err
		}
		skills = append(skills, s)
	}
	return skills, nil
}

func getExperience(db *sql.DB) ([]Experience, error) {
	rows, err := db.Query("SELECT id, title, organization, description, date_range, details FROM experience")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var exps []Experience
	for rows.Next() {
		var e Experience
		var detailsStr string
		if err := rows.Scan(&e.ID, &e.Title, &e.Organization, &e.Description, &e.DateRange, &detailsStr); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(detailsStr), &e.Details)
		exps = append(exps, e)
	}
	return exps, nil
}

func getEducation(db *sql.DB) ([]Education, error) {
	rows, err := db.Query("SELECT id, institution, degree, date_range FROM education")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edus []Education
	for rows.Next() {
		var e Education
		if err := rows.Scan(&e.ID, &e.Institution, &e.Degree, &e.DateRange); err != nil {
			return nil, err
		}
		edus = append(edus, e)
	}
	return edus, nil
}

func getCourses(db *sql.DB) ([]Course, error) {
	rows, err := db.Query("SELECT id, name, institution, date_range FROM courses")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var courses []Course
	for rows.Next() {
		var c Course
		if err := rows.Scan(&c.ID, &c.Name, &c.Institution, &c.DateRange); err != nil {
			return nil, err
		}
		courses = append(courses, c)
	}
	return courses, nil
}

func insertContactMessage(db *sql.DB, name, email, message string) error {
	_, err := db.Exec("INSERT INTO contact_messages (name, email, message) VALUES (?, ?, ?)", name, email, message)
	return err
}
