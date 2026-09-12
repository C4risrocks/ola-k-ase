package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/argon2"
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
	IsRead    bool
}

type Post struct {
	ID          int
	Slug        string
	Title       string
	Summary     string
	Content     string
	Tags        []string
	PublishedAt string
	ReadingTime string
}

func openDB(dataSourceName string) (*sql.DB, error) {
	// Ensure the directory exists and is only accessible to the service user.
	dir := filepath.Dir(dataSourceName)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, fmt.Errorf("secure data directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dataSourceName)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	// High-performance PRAGMAs for production SQLite
	pragmas := `
	PRAGMA journal_mode = WAL;
	PRAGMA synchronous = NORMAL;
	PRAGMA foreign_keys = ON;
	PRAGMA busy_timeout = 5000;
	PRAGMA temp_store = MEMORY;
	`
	if _, err := db.Exec(pragmas); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure sqlite pragmas: %w", err)
	}

	if err := os.Chmod(dataSourceName, 0600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("secure database file: %w", err)
	}

	return db, nil
}

func initDB(dataSourceName string) (*sql.DB, error) {
	db, err := openDB(dataSourceName)
	if err != nil {
		return nil, err
	}

	if err := applyMigrations(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := seedData(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := seedPosts(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func applyMigrations(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create schema migrations table: %w", err)
	}

	migrations, err := fs.Glob(embeddedFS, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(migrations)

	for _, migrationPath := range migrations {
		version := strings.TrimSuffix(strings.TrimPrefix(migrationPath, "migrations/"), ".sql")
		var applied bool
		if err := db.QueryRow("SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", version).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %q: %w", version, err)
		}
		if applied {
			continue
		}

		contents, err := fs.ReadFile(embeddedFS, migrationPath)
		if err != nil {
			return fmt.Errorf("read migration %q: %w", version, err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %q: %w", version, err)
		}
		if _, err := tx.Exec(string(contents)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %q: %w", version, err)
		}
		if _, err := tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %q: %w", version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %q: %w", version, err)
		}
	}

	return nil
}

func seedData(db *sql.DB) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM profile").Scan(&count); err != nil {
		return fmt.Errorf("count profile: %w", err)
	}
	if count > 0 {
		return nil
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin seed transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.Exec(`INSERT INTO profile (name, title, email, phone, github, nationality, about_text)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"Christian Moreno",
		"Computer Engineering Student",
		"c4risrocks@gmail.com",
		"5514938997",
		"www.github.com/C4risrocks",
		"Mexican",
		"Technology Enthusiast usually likes to play videogames, pizza designer with passion for coding, music lover since forever. Always with the hunger of acquire knowledge through investigation and research. Crazy about Gadgets."); err != nil {
		return fmt.Errorf("seed profile: %w", err)
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
		if _, err := tx.Exec("INSERT INTO skills (category, name, items) VALUES (?, ?, ?)", s.Category, s.Name, s.Items); err != nil {
			return fmt.Errorf("seed skill %q: %w", s.Category, err)
		}
	}

	detailsJSON, err := json.Marshal([]string{
		"Web Development for telecomfi.unam.mx",
		"Database support (Oracle Database 11g, Oracle SQL Developer)",
		"Maintenance of Cisco devices at Cisco Interconnectivity Laboratory",
		"Helping Instructors with CCNA and Security Network labs",
		"GRE tunnel implementations (IPv4/IPv6)",
		"AAA Authentication/Authorization (Local and Radius Server)",
		"Radius Server with freeradius and Windows Server (Active Directory)",
	})
	if err != nil {
		return fmt.Errorf("marshal experience details: %w", err)
	}

	if _, err := tx.Exec(`INSERT INTO experience (title, organization, description, date_range, details)
		VALUES (?, ?, ?, ?, ?)`,
		"Social Service",
		"Telecommunications Department, Facultad de Ingeniería, UNAM",
		"Development, Networking and Documentation",
		"February 2019 - Present",
		string(detailsJSON)); err != nil {
		return fmt.Errorf("seed experience: %w", err)
	}

	if _, err := tx.Exec(`INSERT INTO education (institution, degree, date_range) VALUES (?, ?, ?)`,
		"Facultad de Ingeniería, Universidad Nacional Autónoma de México",
		"Computer Engineering",
		""); err != nil {
		return fmt.Errorf("seed education: %w", err)
	}

	courses := []Course{
		{Name: "Hardware Design in VHDL for FPGAs", Institution: "INTESC COURSES", DateRange: "Jan 2019 - Feb 2019"},
		{Name: "CCNA Enterprise", Institution: "Facultad de Ingeniería, UNAM", DateRange: "Aug 2019 - Dec 2019"},
		{Name: "CCNP Security", Institution: "Facultad de Ingeniería, UNAM", DateRange: "Feb 2020 - Present"},
		{Name: "CCNA Enterprise", Institution: "Virtual Education Community Program, Cisco", DateRange: "Mar 2020 - Present"},
	}

	for _, c := range courses {
		if _, err := tx.Exec("INSERT INTO courses (name, institution, date_range) VALUES (?, ?, ?)", c.Name, c.Institution, c.DateRange); err != nil {
			return fmt.Errorf("seed course %q: %w", c.Name, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed transaction: %w", err)
	}
	committed = true
	return nil
}

func seedPosts(db *sql.DB) error {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&count); err != nil {
		return fmt.Errorf("count posts: %w", err)
	}
	if count > 0 {
		return nil
	}

	posts := []struct {
		Slug        string
		Title       string
		Summary     string
		Content     string
		Tags        []string
		PublishedAt string
		ReadingTime string
	}{
		{
			Slug:        "high-performance-sqlite-go-services",
			Title:       "High-Performance SQLite Microservices in Go",
			Summary:     "How to leverage SQLite WAL mode, single connection slot, and Go embed for ultra-fast, zero-overhead deployments.",
			Content:     "SQLite is often overlooked as a production database, but when combined with WAL journal mode (PRAGMA journal_mode = WAL), synchronous = NORMAL, and Go's standard net/http package, it becomes an unbeatable stack for low-latency web services.\n\nKey Optimizations:\n- WAL Mode: Writes don't block reads, enabling concurrent read transactions.\n- Busy Timeout: Prevents SQLITE_BUSY errors under bursty traffic.\n- Single Connection Slot: Setting SetMaxOpenConns(1) avoids lock contention in Go's connection pool.\n- Embedded Binaries: Combining //go:embed with SQLite creates self-contained deployments.",
			Tags:        []string{"GO", "SQLITE", "SYSTEMS"},
			PublishedAt: "2026-07-28",
			ReadingTime: "4 min read",
		},
		{
			Slug:        "vlan-segmentation-8021q-enterprise-design",
			Title:       "VLAN Segmentation & 802.1Q Protocol in Enterprise Networks",
			Summary:     "Deep dive into Layer 2 isolation, trunking encapsulation, and spanning-tree optimizations for mission-critical infrastructure.",
			Content:     "Virtual LANs (VLANs) divide physical broadcast domains into logical segments, improving network security and bandwidth utilization.\n\nIEEE 802.1Q Tagging:\nWhen frames cross a switch trunk line, an 802.1Q header inserts a 4-byte tag into the Ethernet frame header:\n- TPID (0x8100): Tag Protocol Identifier\n- VLAN ID (12 bits): Supports up to 4094 distinct VLANs\n\nBy enforcing strict PVST+ or MSTP topologies alongside AAA-based 802.1X dynamic VLAN assignments, campus networks maintain defense-in-depth security.",
			Tags:        []string{"NETWORKING", "CISCO", "SECURITY"},
			PublishedAt: "2026-06-15",
			ReadingTime: "6 min read",
		},
		{
			Slug:        "zero-framework-htmx-alpine-web-architecture",
			Title:       "Zero-Framework Web Architecture: HTMX + Alpine.js",
			Summary:     "Eliminating heavy SPA bundles by serving hypermedia partials over standard HTTP with minimal client-side state.",
			Content:     "Modern web development has drifted into excessive frontend JavaScript bundlers. HTMX returns to HTML-first architecture by turning any HTML element into an AJAX trigger.\n\nWhy HTMX + Alpine.js Works:\n- HTMX: Handles server interactions, partial HTML swapping (hx-swap=\"innerHTML\"), and lazy loading (hx-trigger=\"revealed\").\n- Alpine.js: Handles short-lived UI state (toggle menus, modal visibility, dynamic scroll observers).\n- Zero Build Step: No webpack, no vite, no node_modules in production. Fast, lightweight, and maintainable.",
			Tags:        []string{"HTMX", "ALPINE.JS", "WEB"},
			PublishedAt: "2026-05-10",
			ReadingTime: "3 min read",
		},
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin seed posts transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	for _, p := range posts {
		tagsJSON, err := json.Marshal(p.Tags)
		if err != nil {
			return fmt.Errorf("marshal tags for post %q: %w", p.Slug, err)
		}
		if _, err := tx.Exec(`INSERT INTO posts (slug, title, summary, content, tags, published_at, reading_time)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			p.Slug, p.Title, p.Summary, p.Content, string(tagsJSON), p.PublishedAt, p.ReadingTime); err != nil {
			return fmt.Errorf("seed post %q: %w", p.Slug, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed posts transaction: %w", err)
	}
	committed = true
	return nil
}

// Data access functions
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
	defer func() { _ = rows.Close() }()

	var skills []Skill
	for rows.Next() {
		var s Skill
		if err := rows.Scan(&s.ID, &s.Category, &s.Name, &s.Items); err != nil {
			return nil, err
		}
		skills = append(skills, s)
	}
	return skills, rows.Err()
}

func getExperience(db *sql.DB) ([]Experience, error) {
	rows, err := db.Query("SELECT id, title, organization, description, date_range, details FROM experience")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var exps []Experience
	for rows.Next() {
		var e Experience
		var detailsStr string
		if err := rows.Scan(&e.ID, &e.Title, &e.Organization, &e.Description, &e.DateRange, &detailsStr); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(detailsStr), &e.Details); err != nil {
			return nil, fmt.Errorf("decode experience details: %w", err)
		}
		exps = append(exps, e)
	}
	return exps, rows.Err()
}

func getEducation(db *sql.DB) ([]Education, error) {
	rows, err := db.Query("SELECT id, institution, degree, date_range FROM education")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var edus []Education
	for rows.Next() {
		var e Education
		if err := rows.Scan(&e.ID, &e.Institution, &e.Degree, &e.DateRange); err != nil {
			return nil, err
		}
		edus = append(edus, e)
	}
	return edus, rows.Err()
}

func getCourses(db *sql.DB) ([]Course, error) {
	rows, err := db.Query("SELECT id, name, institution, date_range FROM courses")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var courses []Course
	for rows.Next() {
		var c Course
		if err := rows.Scan(&c.ID, &c.Name, &c.Institution, &c.DateRange); err != nil {
			return nil, err
		}
		courses = append(courses, c)
	}
	return courses, rows.Err()
}

func insertContactMessage(db *sql.DB, name, email, message string) error {
	_, err := db.Exec("INSERT INTO contact_messages (name, email, message) VALUES (?, ?, ?)", name, email, message)
	return err
}

func getPosts(db *sql.DB) ([]Post, error) {
	rows, err := db.Query("SELECT id, slug, title, summary, content, tags, published_at, reading_time FROM posts ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var posts []Post
	for rows.Next() {
		var p Post
		var tagsStr string
		if err := rows.Scan(&p.ID, &p.Slug, &p.Title, &p.Summary, &p.Content, &tagsStr, &p.PublishedAt, &p.ReadingTime); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(tagsStr), &p.Tags); err != nil {
			p.Tags = []string{tagsStr}
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}

func getPostBySlug(db *sql.DB, slug string) (Post, error) {
	var p Post
	var tagsStr string
	err := db.QueryRow("SELECT id, slug, title, summary, content, tags, published_at, reading_time FROM posts WHERE slug = ?", slug).
		Scan(&p.ID, &p.Slug, &p.Title, &p.Summary, &p.Content, &tagsStr, &p.PublishedAt, &p.ReadingTime)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal([]byte(tagsStr), &p.Tags); err != nil {
		p.Tags = []string{tagsStr}
	}
	return p, nil
}

const (
	minAdminPasswordLength = 12
	maxAdminPasswordLength = 128

	argon2IDMemory      = 64 * 1024
	argon2IDIterations  = 3
	argon2IDParallelism = 2
	argon2IDSaltLength  = 16
	argon2IDKeyLength   = 32

	argon2IDMinMemory      = 8 * 1024
	argon2IDMaxMemory      = 256 * 1024
	argon2IDMinIterations  = 1
	argon2IDMaxIterations  = 10
	argon2IDMinParallelism = 1
	argon2IDMaxParallelism = 8
	argon2IDMinSaltLength  = 8
	argon2IDMaxSaltLength  = 64
	argon2IDMinKeyLength   = 16
	argon2IDMaxKeyLength   = 64
)

type argon2IDParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func validateAdminPassword(password string) error {
	if utf8.RuneCountInString(password) < minAdminPasswordLength {
		return fmt.Errorf("password must be at least %d characters", minAdminPasswordLength)
	}
	if len(password) > maxAdminPasswordLength {
		return fmt.Errorf("password must be at most %d bytes", maxAdminPasswordLength)
	}
	return nil
}

func hashPassword(password string) (string, error) {
	salt := make([]byte, argon2IDSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argon2IDIterations, argon2IDMemory, argon2IDParallelism, argon2IDKeyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argon2IDMemory,
		argon2IDIterations,
		argon2IDParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func parsePasswordHash(encoded string) (argon2IDParams, []byte, []byte, error) {
	var params argon2IDParams

	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return params, nil, nil, fmt.Errorf("unsupported password hash format")
	}

	version, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || version != argon2.Version {
		return params, nil, nil, fmt.Errorf("unsupported argon2 version")
	}

	values := strings.Split(parts[3], ",")
	if len(values) != 3 {
		return params, nil, nil, fmt.Errorf("invalid argon2 parameters")
	}

	memory, err := parseArgon2Parameter(values[0], "m=")
	if err != nil {
		return params, nil, nil, err
	}
	iterations, err := parseArgon2Parameter(values[1], "t=")
	if err != nil {
		return params, nil, nil, err
	}
	parallelism, err := parseArgon2Parameter(values[2], "p=")
	if err != nil {
		return params, nil, nil, err
	}

	if memory < argon2IDMinMemory || memory > argon2IDMaxMemory {
		return params, nil, nil, fmt.Errorf("argon2 memory out of range")
	}
	if iterations < argon2IDMinIterations || iterations > argon2IDMaxIterations {
		return params, nil, nil, fmt.Errorf("argon2 iterations out of range")
	}
	if parallelism < argon2IDMinParallelism || parallelism > argon2IDMaxParallelism {
		return params, nil, nil, fmt.Errorf("argon2 parallelism out of range")
	}
	params.memory = uint32(memory)
	params.iterations = uint32(iterations)
	params.parallelism = uint8(parallelism)

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return params, nil, nil, fmt.Errorf("decode password salt: %w", err)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return params, nil, nil, fmt.Errorf("decode password key: %w", err)
	}
	if len(salt) < argon2IDMinSaltLength || len(salt) > argon2IDMaxSaltLength {
		return params, nil, nil, fmt.Errorf("password salt length out of range")
	}
	if len(key) < argon2IDMinKeyLength || len(key) > argon2IDMaxKeyLength {
		return params, nil, nil, fmt.Errorf("password key length out of range")
	}

	return params, salt, key, nil
}

func parseArgon2Parameter(value, prefix string) (uint64, error) {
	raw, ok := strings.CutPrefix(value, prefix)
	if !ok || raw == "" {
		return 0, fmt.Errorf("invalid argon2 parameter %q", value)
	}
	parsed, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid argon2 parameter %q: %w", value, err)
	}
	return parsed, nil
}

func verifyPassword(encoded, password string) bool {
	params, salt, key, err := parsePasswordHash(encoded)
	if err != nil {
		return false
	}

	computed := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, uint32(len(key)))
	return subtle.ConstantTimeCompare(computed, key) == 1
}

func setAdminPassword(db *sql.DB, username, password string) error {
	if err := validateAdminPassword(password); err != nil {
		return err
	}

	encoded, err := hashPassword(password)
	if err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin admin password update: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.Exec(`
		INSERT INTO admin_users (username, password_hash) VALUES (?, ?)
		ON CONFLICT(username) DO UPDATE SET password_hash = excluded.password_hash`,
		username, encoded); err != nil {
		return fmt.Errorf("upsert admin user: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM sessions"); err != nil {
		return fmt.Errorf("invalidate sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit admin password update: %w", err)
	}
	committed = true

	return nil
}

func authenticateAdmin(db *sql.DB, username, password string) bool {
	var stored string
	err := db.QueryRow("SELECT password_hash FROM admin_users WHERE username = ?", username).Scan(&stored)
	if err != nil {
		return false
	}
	return verifyPassword(stored, password)
}

func hasSecureAdmin(db *sql.DB) (bool, error) {
	rows, err := db.Query("SELECT password_hash FROM admin_users")
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	total := 0
	valid := 0
	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			return false, err
		}
		total++
		if _, _, _, err := parsePasswordHash(encoded); err == nil {
			valid++
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}

	return total > 0 && valid == total, nil
}

const (
	sessionTokenBytes = 32
	sessionTTL        = 24 * time.Hour
)

func randomToken() (string, error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func hashSessionToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type sessionRecord struct {
	Username string
	CSRFHash string
}

func createSession(db *sql.DB, username string) (string, string, error) {
	token, err := randomToken()
	if err != nil {
		return "", "", fmt.Errorf("generate session token: %w", err)
	}
	csrfToken, err := randomToken()
	if err != nil {
		return "", "", fmt.Errorf("generate csrf token: %w", err)
	}

	now := time.Now().UTC()
	tx, err := db.Begin()
	if err != nil {
		return "", "", fmt.Errorf("begin session transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.Exec("DELETE FROM sessions WHERE expires_at < ?", now.Format(time.RFC3339)); err != nil {
		return "", "", fmt.Errorf("cleanup expired sessions: %w", err)
	}

	if _, err := tx.Exec("INSERT INTO sessions (id, username, expires_at, csrf_hash) VALUES (?, ?, ?, ?)",
		hashSessionToken(token), username, now.Add(sessionTTL).Format(time.RFC3339), hashSessionToken(csrfToken)); err != nil {
		return "", "", fmt.Errorf("insert session: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", "", fmt.Errorf("commit session: %w", err)
	}
	committed = true

	return token, csrfToken, nil
}

func lookupSession(db *sql.DB, token string) (sessionRecord, bool) {
	if token == "" {
		return sessionRecord{}, false
	}

	var record sessionRecord
	var expiresAtStr string
	err := db.QueryRow(`
		SELECT s.username, s.expires_at, s.csrf_hash
		FROM sessions s
		JOIN admin_users a ON a.username = s.username
		WHERE s.id = ?`, hashSessionToken(token)).Scan(&record.Username, &expiresAtStr, &record.CSRFHash)
	if err != nil {
		return sessionRecord{}, false
	}

	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil || time.Now().After(expiresAt) {
		_, _ = db.Exec("DELETE FROM sessions WHERE id = ?", hashSessionToken(token))
		return sessionRecord{}, false
	}
	return record, true
}

func validateSession(db *sql.DB, token string) (string, bool) {
	record, ok := lookupSession(db, token)
	return record.Username, ok
}

func csrfMatches(record sessionRecord, csrfToken string) bool {
	if csrfToken == "" || record.CSRFHash == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hashSessionToken(csrfToken)), []byte(record.CSRFHash)) == 1
}

func deleteSession(db *sql.DB, token string) error {
	_, err := db.Exec("DELETE FROM sessions WHERE id = ?", hashSessionToken(token))
	return err
}

func createPost(db *sql.DB, p Post) error {
	tagsJSON, err := json.Marshal(p.Tags)
	if err != nil {
		return err
	}
	if p.ReadingTime == "" {
		p.ReadingTime = "3 min read"
	}
	if p.PublishedAt == "" {
		p.PublishedAt = time.Now().Format("2006-01-02")
	}
	_, err = db.Exec(`INSERT INTO posts (slug, title, summary, content, tags, published_at, reading_time)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.Slug, p.Title, p.Summary, p.Content, string(tagsJSON), p.PublishedAt, p.ReadingTime)
	return err
}

func getContactMessages(db *sql.DB) ([]ContactMessage, error) {
	rows, err := db.Query("SELECT id, name, email, message, created_at, is_read FROM contact_messages ORDER BY id DESC")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var msgs []ContactMessage
	for rows.Next() {
		var m ContactMessage
		if err := rows.Scan(&m.ID, &m.Name, &m.Email, &m.Message, &m.CreatedAt, &m.IsRead); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func markContactMessageRead(db *sql.DB, id int) error {
	_, err := db.Exec("UPDATE contact_messages SET is_read = 1 WHERE id = ?", id)
	return err
}

func deleteContactMessage(db *sql.DB, id int) error {
	_, err := db.Exec("DELETE FROM contact_messages WHERE id = ?", id)
	return err
}

func updatePost(db *sql.DB, p Post) error {
	tagsJSON, err := json.Marshal(p.Tags)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE posts SET title = ?, summary = ?, content = ?, tags = ?, slug = ? WHERE id = ?`,
		p.Title, p.Summary, p.Content, string(tagsJSON), p.Slug, p.ID)
	return err
}

func deletePost(db *sql.DB, id int) error {
	_, err := db.Exec("DELETE FROM posts WHERE id = ?", id)
	return err
}
