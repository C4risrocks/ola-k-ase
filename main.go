package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"time"
)

var db *sql.DB
var templates *template.Template

const (
	maxContactBodyBytes  = 16 << 10
	maxContactNameLen    = 100
	maxContactEmailLen   = 254
	maxContactMessageLen = 4000
)

func main() {
	var err error
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "portfolio.db"
	}

	db, err = initDB(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Parse templates
	templates = template.Must(template.ParseGlob("templates/*.html"))
	template.Must(templates.ParseGlob("templates/partials/*.html"))

	// Serve static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// Routes
	http.HandleFunc("/", indexHandler)
	http.HandleFunc("/api/section/", sectionHandler)
	http.HandleFunc("/api/contact", contactHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           nil,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("Portfolio server running at http://localhost:%s", port)
	log.Fatal(srv.ListenAndServe())
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	profile, err := getProfile(db)
	if err != nil && err != sql.ErrNoRows {
		log.Println("DB error:", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	data := struct {
		Profile Profile
	}{
		Profile: profile,
	}

	renderTemplate(w, "index.html", data)
}

func sectionHandler(w http.ResponseWriter, r *http.Request) {
	section := strings.TrimPrefix(r.URL.Path, "/api/section/")
	var data interface{}
	var templateName string
	var err error

	switch section {
	case "about":
		data, err = getProfile(db)
		templateName = "about.html"
	case "skills":
		data, err = getSkills(db)
		templateName = "skills.html"
	case "experience":
		data, err = getExperience(db)
		templateName = "experience.html"
	case "education":
		var edus []Education
		var courses []Course
		edus, err = getEducation(db)
		if err == nil {
			courses, err = getCourses(db)
		}
		data = struct {
			Education []Education
			Courses   []Course
		}{
			Education: edus,
			Courses:   courses,
		}
		templateName = "education.html"
	case "contact":
		templateName = "contact.html"
	default:
		http.NotFound(w, r)
		return
	}

	if err != nil {
		log.Println("DB error:", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	renderTemplate(w, templateName, data)
}

func contactHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxContactBodyBytes)
	if err := r.ParseForm(); err != nil {
		writeContactAlert(w, http.StatusBadRequest, "error", "ph-warning", "Invalid form data. Please try again.")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.TrimSpace(r.FormValue("email"))
	message := strings.TrimSpace(r.FormValue("message"))

	if validationMessage := contactValidationError(name, email, message); validationMessage != "" {
		writeContactAlert(w, http.StatusUnprocessableEntity, "error", "ph-warning", validationMessage)
		return
	}

	if err := insertContactMessage(db, name, email, message); err != nil {
		log.Println("DB error:", err)
		writeContactAlert(w, http.StatusInternalServerError, "error", "ph-warning", "An error occurred. Please try again later.")
		return
	}

	writeContactAlert(w, http.StatusOK, "success", "ph-check-circle", "Thank you, "+template.HTMLEscapeString(name)+"! Your message has been sent successfully.")
}

func contactValidationError(name, email, message string) string {
	if name == "" || email == "" || message == "" {
		return "All fields are required."
	}
	if len(name) > maxContactNameLen {
		return "Name is too long."
	}
	if len(email) > maxContactEmailLen || !strings.Contains(email, ".") {
		return "Please enter a valid email address."
	}
	if len(message) > maxContactMessageLen {
		return "Message is too long."
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "Please enter a valid email address."
	}
	return ""
}

func writeContactAlert(w http.ResponseWriter, status int, kind, icon, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<div class="form__alert form__alert--%s"><i class="ph %s" aria-hidden="true"></i>%s</div>`, kind, icon, message)
}

func renderTemplate(w http.ResponseWriter, name string, data interface{}) {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		log.Println("Template error:", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}
