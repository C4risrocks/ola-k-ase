package main

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/mail"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed templates/* templates/partials/*
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

var db *sql.DB
var templates *template.Template
var appConfig Config

const (
	maxContactBodyBytes  = 16 << 10
	maxContactNameLen    = 100
	maxContactEmailLen   = 254
	maxContactMessageLen = 4000
)

func initLogger(env string, level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler

	if env == "production" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))
}

func main() {
	appConfig = LoadConfig()
	initLogger(appConfig.AppEnv, appConfig.LogLevel)

	slog.Info("starting application",
		slog.String("version", Version),
		slog.String("commit", Commit),
		slog.String("env", appConfig.AppEnv))

	var err error
	db, err = initDB(appConfig.DatabasePath)
	if err != nil {
		slog.Error("failed to initialize database", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("error closing database", slog.Any("error", err))
		}
	}()

	// Parse templates from embed.FS
	templates, err = template.ParseFS(templateFS, "templates/*.html", "templates/partials/*.html")
	if err != nil {
		slog.Error("failed to parse templates", slog.Any("error", err))
		os.Exit(1)
	}

	mux := http.NewServeMux()

	// Serve static files from embed.FS
	// staticFS root is the project root, so staticFS.Open("static/...") matches URL path
	mux.Handle("/static/", cacheMiddleware(http.FileServer(http.FS(staticFS))))

	// Application routes
	mux.Handle("/", cacheMiddleware(http.HandlerFunc(indexHandler)))
	mux.Handle("/api/section/", cacheMiddleware(http.HandlerFunc(sectionHandler)))
	mux.Handle("/api/contact", http.HandlerFunc(contactHandler))

	// Operational endpoints
	mux.Handle("/metrics", metricsHandler())
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ready", readyHandler)
	mux.HandleFunc("/version", versionHandler)

	// Wrap mux with global middleware
	handler := standardMiddleware(mux)

	srv := &http.Server{
		Addr:              ":" + appConfig.Port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	// Graceful Shutdown
	go func() {
		slog.Info("server listening", slog.String("port", appConfig.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down server gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", slog.Any("error", err))
	}
	slog.Info("server exited cleanly")
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	profile, err := getProfile(db)
	if err != nil && err != sql.ErrNoRows {
		slog.Error("db error", slog.Any("error", err))
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
		slog.Error("db error", slog.Any("error", err))
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
		slog.Error("db error", slog.Any("error", err))
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
		slog.Error("template error", slog.Any("error", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// Health, Ready & Version Endpoints
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func readyHandler(w http.ResponseWriter, r *http.Request) {
	if err := db.Ping(); err != nil {
		slog.Error("db ping failed", slog.Any("error", err))
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Ready"))
}

func versionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"version":    Version,
		"commit":     Commit,
		"build_date": BuildDate,
	})
}
