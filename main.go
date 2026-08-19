package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/mail"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"portfolio/internal/buildinfo"
)

//go:embed templates/* templates/partials/* static/* migrations/*.sql
var embeddedFS embed.FS

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
	if err := run(); err != nil {
		slog.Error("application stopped", slog.Any("error", err))
		os.Exit(1)
	}
}

func run() error {
	appConfig = LoadConfig()
	initLogger(appConfig.AppEnv, appConfig.LogLevel)

	slog.Info("starting application",
		slog.String("version", buildinfo.Version),
		slog.String("commit", buildinfo.Commit),
		slog.String("env", appConfig.AppEnv))

	var err error
	db, err = initDB(appConfig.DatabasePath)
	if err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("error closing database", slog.Any("error", err))
		}
	}()

	// Parse templates from embed.FS
	templates, err = parseTemplates()
	if err != nil {
		return fmt.Errorf("parse templates: %w", err)
	}

	assetETags, err := buildAssetETags()
	if err != nil {
		return fmt.Errorf("build asset etags: %w", err)
	}

	mux := http.NewServeMux()

	// Serve static files from embed.FS
	// embeddedFS root is the project root, so embeddedFS.Open("static/...") matches URL path.
	mux.Handle("/static/", http.FileServer(http.FS(embeddedFS)))

	// Application routes
	mux.HandleFunc("/", indexHandler)
	mux.HandleFunc("/post/", postPageHandler)
	mux.HandleFunc("/api/section/", sectionHandler)
	mux.HandleFunc("/api/post/", postDetailHandler)
	mux.HandleFunc("/api/login", loginHandler)
	mux.HandleFunc("/api/logout", logoutHandler)
	mux.HandleFunc("/api/admin/dashboard", adminDashboardHandler)
	mux.HandleFunc("/api/admin/posts", adminCreatePostHandler)
	mux.HandleFunc("/api/admin/posts/edit/", adminEditPostFormHandler)
	mux.HandleFunc("/api/admin/posts/update/", adminUpdatePostHandler)
	mux.HandleFunc("/api/admin/posts/delete/", adminDeletePostHandler)
	mux.HandleFunc("/api/admin/messages/read/", adminMarkMessageReadHandler)
	mux.HandleFunc("/api/admin/messages/delete/", adminDeleteMessageHandler)
	mux.Handle("/api/contact", http.HandlerFunc(contactHandler))

	// Operational endpoints
	mux.Handle("/metrics", metricsHandler())
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ready", readyHandler)
	mux.HandleFunc("/version", versionHandler)

	// Wrap mux with global middleware. Cache policy applies to every route.
	handler := standardMiddleware(compressionMiddleware(cacheMiddleware(mux, assetETags)))

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
	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server listening", slog.String("port", appConfig.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	select {
	case sig := <-quit:
		slog.Info("shutting down server gracefully", slog.String("signal", sig.String()))
	case err := <-serverErr:
		return fmt.Errorf("listen: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}
	slog.Info("server exited cleanly")
	return nil
}

func buildAssetETags() (map[string]string, error) {
	etags := make(map[string]string)
	err := fs.WalkDir(embeddedFS, "static", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}

		contents, err := fs.ReadFile(embeddedFS, path)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(contents)
		etags["/"+path] = fmt.Sprintf(`"%x"`, hash)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return etags, nil
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	profile, err := getProfile(db)
	if err != nil && err != sql.ErrNoRows {
		slog.Error("db error getting profile", slog.Any("error", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	skills, err := getSkills(db)
	if err != nil {
		slog.Error("db error getting skills", slog.Any("error", err))
	}

	experiences, err := getExperience(db)
	if err != nil {
		slog.Error("db error getting experience", slog.Any("error", err))
	}

	educations, err := getEducation(db)
	if err != nil {
		slog.Error("db error getting education", slog.Any("error", err))
	}

	courses, err := getCourses(db)
	if err != nil {
		slog.Error("db error getting courses", slog.Any("error", err))
	}

	posts, err := getPosts(db)
	if err != nil {
		slog.Error("db error getting posts", slog.Any("error", err))
	}

	cookie, _ := r.Cookie("session_token")
	var token string
	if cookie != nil {
		token = cookie.Value
	}
	_, isAdmin := validateSession(db, token)

	educationData := struct {
		Education []Education
		Courses   []Course
	}{
		Education: educations,
		Courses:   courses,
	}

	writingData := struct {
		Posts   []Post
		IsAdmin bool
	}{
		Posts:   posts,
		IsAdmin: isAdmin,
	}

	data := struct {
		Profile       Profile
		Skills        []Skill
		Experiences   []Experience
		EducationData interface{}
		Posts         []Post
		WritingData   interface{}
		IsAdmin       bool
	}{
		Profile:       profile,
		Skills:        skills,
		Experiences:   experiences,
		EducationData: educationData,
		Posts:         posts,
		WritingData:   writingData,
		IsAdmin:       isAdmin,
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
	case "writing":
		var posts []Post
		posts, err = getPosts(db)
		cookie, _ := r.Cookie("session_token")
		var token string
		if cookie != nil {
			token = cookie.Value
		}
		_, isAdmin := validateSession(db, token)
		data = struct {
			Posts   []Post
			IsAdmin bool
		}{
			Posts:   posts,
			IsAdmin: isAdmin,
		}
		templateName = "writing.html"
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

func postDetailHandler(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/api/post/")
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	post, err := getPostBySlug(db, slug)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	} else if err != nil {
		slog.Error("db error", slog.Any("error", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	renderTemplate(w, "post_detail.html", post)
}

func postPageHandler(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/post/")
	if slug == "" {
		http.NotFound(w, r)
		return
	}

	post, err := getPostBySlug(db, slug)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	} else if err != nil {
		slog.Error("db error", slog.Any("error", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	renderTemplate(w, "post_page.html", post)
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeLoginAlert(w, http.StatusBadRequest, "error", "Invalid form data.")
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := strings.TrimSpace(r.FormValue("password"))

	if username == "" || password == "" {
		writeLoginAlert(w, http.StatusUnprocessableEntity, "error", "Username and password are required.")
		return
	}

	if !authenticateAdmin(db, username, password) {
		writeLoginAlert(w, http.StatusUnauthorized, "error", "Invalid credentials.")
		return
	}

	token, err := createSession(db, username)
	if err != nil {
		slog.Error("session creation failed", slog.Any("error", err))
		writeLoginAlert(w, http.StatusInternalServerError, "error", "Could not create session.")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	w.Header().Set("HX-Refresh", "true")
	writeLoginAlert(w, http.StatusOK, "success", "Logged in successfully!")
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_token")
	if err == nil && cookie != nil {
		_ = deleteSession(db, cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_token",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	if r.Header.Get("HX-Request") != "" {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func adminCreatePostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, _ := r.Cookie("session_token")
	var token string
	if cookie != nil {
		token = cookie.Value
	}
	_, isAdmin := validateSession(db, token)
	if !isAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		writePostAlert(w, http.StatusBadRequest, "error", "Invalid form data.")
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	slug := strings.TrimSpace(r.FormValue("slug"))
	summary := strings.TrimSpace(r.FormValue("summary"))
	content := strings.TrimSpace(r.FormValue("content"))
	tagsRaw := strings.TrimSpace(r.FormValue("tags"))

	if title == "" || summary == "" || content == "" {
		writePostAlert(w, http.StatusUnprocessableEntity, "error", "Title, summary, and content are required.")
		return
	}

	if slug == "" {
		slug = strings.ToLower(strings.ReplaceAll(title, " ", "-"))
	}

	var tags []string
	if tagsRaw != "" {
		for _, tag := range strings.Split(tagsRaw, ",") {
			t := strings.TrimSpace(tag)
			if t != "" {
				tags = append(tags, strings.ToUpper(t))
			}
		}
	}

	wordCount := len(strings.Fields(content))
	readingMins := wordCount / 150
	if readingMins < 1 {
		readingMins = 1
	}

	post := Post{
		Slug:        slug,
		Title:       title,
		Summary:     summary,
		Content:     content,
		Tags:        tags,
		PublishedAt: time.Now().Format("2006-01-02"),
		ReadingTime: fmt.Sprintf("%d min read", readingMins),
	}

	if err := createPost(db, post); err != nil {
		slog.Error("create post failed", slog.Any("error", err))
		writePostAlert(w, http.StatusInternalServerError, "error", "Error publishing post. Slug may already exist.")
		return
	}

	w.Header().Set("HX-Trigger", "postCreated")
	writePostAlert(w, http.StatusOK, "success", "Article published successfully!")
}

func writeLoginAlert(w http.ResponseWriter, status int, kind, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<div class="form__alert form__alert--%s">%s</div>`, kind, message)
}

func writePostAlert(w http.ResponseWriter, status int, kind, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<div class="form__alert form__alert--%s">%s</div>`, kind, message)
}

func adminDashboardHandler(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie("session_token")
	var token string
	if cookie != nil {
		token = cookie.Value
	}
	_, isAdmin := validateSession(db, token)
	if !isAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	posts, err := getPosts(db)
	if err != nil {
		slog.Error("db error getting posts", slog.Any("error", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	messages, err := getContactMessages(db)
	if err != nil {
		slog.Error("db error getting messages", slog.Any("error", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	unreadCount := 0
	for _, m := range messages {
		if !m.IsRead {
			unreadCount++
		}
	}

	data := struct {
		Posts       []Post
		Messages    []ContactMessage
		UnreadCount int
		TotalPosts  int
		TotalMsgs   int
	}{
		Posts:       posts,
		Messages:    messages,
		UnreadCount: unreadCount,
		TotalPosts:  len(posts),
		TotalMsgs:   len(messages),
	}

	renderTemplate(w, "admin_dashboard.html", data)
}

func adminEditPostFormHandler(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie("session_token")
	var token string
	if cookie != nil {
		token = cookie.Value
	}
	_, isAdmin := validateSession(db, token)
	if !isAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/posts/edit/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	posts, err := getPosts(db)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	var targetPost Post
	found := false
	for _, p := range posts {
		if p.ID == id {
			targetPost = p
			found = true
			break
		}
	}

	if !found {
		http.NotFound(w, r)
		return
	}

	data := struct {
		Post    Post
		TagsStr string
	}{
		Post:    targetPost,
		TagsStr: strings.Join(targetPost.Tags, ", "),
	}

	renderTemplate(w, "admin_edit_post.html", data)
}

func adminUpdatePostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, _ := r.Cookie("session_token")
	var token string
	if cookie != nil {
		token = cookie.Value
	}
	_, isAdmin := validateSession(db, token)
	if !isAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/posts/update/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		writePostAlert(w, http.StatusBadRequest, "error", "Invalid form data.")
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	slug := strings.TrimSpace(r.FormValue("slug"))
	summary := strings.TrimSpace(r.FormValue("summary"))
	content := strings.TrimSpace(r.FormValue("content"))
	tagsRaw := strings.TrimSpace(r.FormValue("tags"))

	if title == "" || summary == "" || content == "" {
		writePostAlert(w, http.StatusUnprocessableEntity, "error", "Title, summary, and content are required.")
		return
	}

	if slug == "" {
		slug = strings.ToLower(strings.ReplaceAll(title, " ", "-"))
	}

	var tags []string
	if tagsRaw != "" {
		for _, tag := range strings.Split(tagsRaw, ",") {
			t := strings.TrimSpace(tag)
			if t != "" {
				tags = append(tags, strings.ToUpper(t))
			}
		}
	}

	post := Post{
		ID:      id,
		Slug:    slug,
		Title:   title,
		Summary: summary,
		Content: content,
		Tags:    tags,
	}

	if err := updatePost(db, post); err != nil {
		slog.Error("update post failed", slog.Any("error", err))
		writePostAlert(w, http.StatusInternalServerError, "error", "Error updating post.")
		return
	}

	w.Header().Set("HX-Trigger", "postUpdated")
	writePostAlert(w, http.StatusOK, "success", "Article updated successfully!")
}

func adminDeletePostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, _ := r.Cookie("session_token")
	var token string
	if cookie != nil {
		token = cookie.Value
	}
	_, isAdmin := validateSession(db, token)
	if !isAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/posts/delete/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := deletePost(db, id); err != nil {
		slog.Error("delete post failed", slog.Any("error", err))
		http.Error(w, "Error deleting post", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Trigger", "postUpdated")
	w.WriteHeader(http.StatusOK)
}

func adminMarkMessageReadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, _ := r.Cookie("session_token")
	var token string
	if cookie != nil {
		token = cookie.Value
	}
	_, isAdmin := validateSession(db, token)
	if !isAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/messages/read/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := markContactMessageRead(db, id); err != nil {
		slog.Error("mark message read failed", slog.Any("error", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Trigger", "messageUpdated")
	w.WriteHeader(http.StatusOK)
}

func adminDeleteMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, _ := r.Cookie("session_token")
	var token string
	if cookie != nil {
		token = cookie.Value
	}
	_, isAdmin := validateSession(db, token)
	if !isAdmin {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/admin/messages/delete/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := deleteContactMessage(db, id); err != nil {
		slog.Error("delete message failed", slog.Any("error", err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("HX-Trigger", "messageUpdated")
	w.WriteHeader(http.StatusOK)
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
	_, _ = fmt.Fprintf(w, `<div class="form__alert form__alert--%s"><i class="ph %s" aria-hidden="true"></i>%s</div>`, kind, icon, message)
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
	if err := checkDataDirectory(appConfig.DatabasePath); err != nil {
		slog.Error("data directory check failed", slog.Any("error", err))
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := db.PingContext(r.Context()); err != nil {
		slog.Error("db ping failed", slog.Any("error", err))
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Ready"))
}

func versionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"version":    buildinfo.Version,
		"commit":     buildinfo.Commit,
		"build_date": buildinfo.BuildDate,
	})
}

func checkDataDirectory(databasePath string) error {
	directory := filepath.Dir(databasePath)
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("stat data directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("data path is not a directory: %s", directory)
	}

	testFile, err := os.CreateTemp(directory, ".ready-*")
	if err != nil {
		return fmt.Errorf("write data directory: %w", err)
	}
	testPath := testFile.Name()
	if err := testFile.Close(); err != nil {
		_ = os.Remove(testPath)
		return fmt.Errorf("close data directory probe: %w", err)
	}
	if err := os.Remove(testPath); err != nil {
		return fmt.Errorf("remove data directory probe: %w", err)
	}
	return nil
}

func parseTemplates() (*template.Template, error) {
	funcMap := template.FuncMap{
		"splitParagraphs": func(s string) []string {
			paras := strings.Split(s, "\n\n")
			var result []string
			for _, p := range paras {
				trimmed := strings.TrimSpace(p)
				if trimmed != "" {
					result = append(result, trimmed)
				}
			}
			return result
		},
	}
	return template.New("").Funcs(funcMap).ParseFS(embeddedFS, "templates/*.html", "templates/partials/*.html")
}
