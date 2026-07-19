package main

import (
	"database/sql"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
)

var db *sql.DB
var templates *template.Template

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

	log.Printf("🚀 Portfolio server running at http://localhost:%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	profile, _ := getProfile(db)

	data := struct {
		Profile Profile
	}{
		Profile: profile,
	}

	err := templates.ExecuteTemplate(w, "index.html", data)
	if err != nil {
		log.Println("Template error:", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func sectionHandler(w http.ResponseWriter, r *http.Request) {
	section := strings.TrimPrefix(r.URL.Path, "/api/section/")
	var data interface{}
	var templateName string

	switch section {
	case "about":
		data, _ = getProfile(db)
		templateName = "about.html"
	case "skills":
		data, _ = getSkills(db)
		templateName = "skills.html"
	case "experience":
		data, _ = getExperience(db)
		templateName = "experience.html"
	case "education":
		edus, _ := getEducation(db)
		courses, _ := getCourses(db)
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

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err := templates.ExecuteTemplate(w, templateName, data)
	if err != nil {
		log.Println("Template error:", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func contactHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div class="form__alert form__alert--error">⚠️ Invalid form data. Please try again.</div>`))
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.TrimSpace(r.FormValue("email"))
	message := strings.TrimSpace(r.FormValue("message"))

	if name == "" || email == "" || message == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div class="form__alert form__alert--error">⚠️ All fields are required.</div>`))
		return
	}

	err = insertContactMessage(db, name, email, message)
	if err != nil {
		log.Println("DB error:", err)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div class="form__alert form__alert--error">⚠️ An error occurred. Please try again later.</div>`))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<div class="form__alert form__alert--success">✅ Thank you, ` + template.HTMLEscapeString(name) + `! Your message has been sent successfully.</div>`))
}
