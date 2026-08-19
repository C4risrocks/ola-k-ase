package main

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestApp(t *testing.T) *sql.DB {
	t.Helper()

	var err error
	databasePath := filepath.Join(t.TempDir(), "portfolio_test.db")
	db, err = initDB(databasePath)
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	templates = template.Must(parseTemplates())
	appConfig = Config{DatabasePath: databasePath}

	return db
}

func postContact(t *testing.T, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/contact", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	contactHandler(rr, req)
	return rr
}

func countRows(t *testing.T, db *sql.DB, query string) int {
	t.Helper()

	var count int
	if err := db.QueryRow(query).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func TestContactRejectsEmptyFields(t *testing.T) {
	db := setupTestApp(t)

	rr := postContact(t, url.Values{
		"name":    {""},
		"email":   {""},
		"message": {""},
	})

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnprocessableEntity)
	}
	if !strings.Contains(rr.Body.String(), "All fields are required.") {
		t.Fatalf("body does not contain required-field message: %q", rr.Body.String())
	}
	if got := countRows(t, db, "SELECT COUNT(*) FROM contact_messages"); got != 0 {
		t.Fatalf("contact_messages count = %d, want 0", got)
	}
}

func TestContactRejectsInvalidEmail(t *testing.T) {
	db := setupTestApp(t)

	rr := postContact(t, url.Values{
		"name":    {"Review"},
		"email":   {"invalid"},
		"message": {"hello"},
	})

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnprocessableEntity)
	}
	if !strings.Contains(rr.Body.String(), "valid email") {
		t.Fatalf("body does not contain email validation message: %q", rr.Body.String())
	}
	if got := countRows(t, db, "SELECT COUNT(*) FROM contact_messages"); got != 0 {
		t.Fatalf("contact_messages count = %d, want 0", got)
	}
}

func TestContactRejectsTooLongMessage(t *testing.T) {
	db := setupTestApp(t)

	rr := postContact(t, url.Values{
		"name":    {"Review"},
		"email":   {"review@example.com"},
		"message": {strings.Repeat("a", maxContactMessageLen+1)},
	})

	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnprocessableEntity)
	}
	if got := countRows(t, db, "SELECT COUNT(*) FROM contact_messages"); got != 0 {
		t.Fatalf("contact_messages count = %d, want 0", got)
	}
}

func TestContactAcceptsValidAndEscapesName(t *testing.T) {
	db := setupTestApp(t)
	rawName := "<b>Review</b>"

	rr := postContact(t, url.Values{
		"name":    {rawName},
		"email":   {"review@example.com"},
		"message": {"hello"},
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %q", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Thank you, &lt;b&gt;Review&lt;/b&gt;!") {
		t.Fatalf("body does not contain escaped name: %q", rr.Body.String())
	}
	if got := countRows(t, db, "SELECT COUNT(*) FROM contact_messages"); got != 1 {
		t.Fatalf("contact_messages count = %d, want 1", got)
	}

	var storedName string
	if err := db.QueryRow("SELECT name FROM contact_messages LIMIT 1").Scan(&storedName); err != nil {
		t.Fatalf("read stored name: %v", err)
	}
	if storedName != rawName {
		t.Fatalf("stored name = %q, want %q", storedName, rawName)
	}
}

func TestContactSQLInjectionPayloadIsStoredAsData(t *testing.T) {
	db := setupTestApp(t)
	payload := "'); DROP TABLE contact_messages; --"

	rr := postContact(t, url.Values{
		"name":    {payload},
		"email":   {"review@example.com"},
		"message": {"hello"},
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %q", rr.Code, http.StatusOK, rr.Body.String())
	}
	if got := countRows(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'contact_messages'"); got != 1 {
		t.Fatalf("contact_messages table count = %d, want 1", got)
	}
	if got := countRows(t, db, "SELECT COUNT(*) FROM contact_messages"); got != 1 {
		t.Fatalf("contact_messages count = %d, want 1", got)
	}
}

func TestContactMethodNotAllowed(t *testing.T) {
	setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/contact", nil)
	rr := httptest.NewRecorder()
	contactHandler(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}

func TestSectionHandlersRenderSeededContent(t *testing.T) {
	setupTestApp(t)

	profile, err := getProfile(db)
	if err != nil {
		t.Fatalf("getProfile: %v", err)
	}

	tests := map[string]string{
		"about":      profile.Name,
		"skills":     "Switching",
		"experience": "Social Service",
		"education":  "Computer Engineering",
		"writing":    "High-Performance SQLite Microservices in Go",
		"contact":    "Send Message",
	}

	for section, want := range tests {
		t.Run(section, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/section/"+section, nil)
			rr := httptest.NewRecorder()
			sectionHandler(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body: %q", rr.Code, http.StatusOK, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), want) {
				t.Fatalf("body does not contain %q: %q", want, rr.Body.String())
			}
		})
	}
}

func TestSectionHandlerUnknownReturns404(t *testing.T) {
	setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/section/nope", nil)
	rr := httptest.NewRecorder()
	sectionHandler(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestGetExperienceDecodesDetails(t *testing.T) {
	setupTestApp(t)

	experiences, err := getExperience(db)
	if err != nil {
		t.Fatalf("getExperience: %v", err)
	}
	if len(experiences) == 0 {
		t.Fatal("getExperience returned no rows")
	}
	if len(experiences[0].Details) == 0 {
		t.Fatal("getExperience returned no decoded details")
	}
}

func TestMigrationsAreRecorded(t *testing.T) {
	db := setupTestApp(t)

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("read schema migrations: %v", err)
	}
	if count != 4 {
		t.Fatalf("schema migrations count = %d, want 4", count)
	}
}

func TestGetPostsAndSlug(t *testing.T) {
	setupTestApp(t)

	posts, err := getPosts(db)
	if err != nil {
		t.Fatalf("getPosts: %v", err)
	}
	if len(posts) == 0 {
		t.Fatal("getPosts returned 0 posts")
	}

	post, err := getPostBySlug(db, posts[0].Slug)
	if err != nil {
		t.Fatalf("getPostBySlug: %v", err)
	}
	if post.Title != posts[0].Title {
		t.Fatalf("slug post title = %q, want %q", post.Title, posts[0].Title)
	}
}

func TestPostDetailHandler(t *testing.T) {
	setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/post/high-performance-sqlite-go-services", nil)
	rr := httptest.NewRecorder()
	postDetailHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %q", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "High-Performance SQLite Microservices in Go") {
		t.Fatalf("body does not contain expected title: %q", rr.Body.String())
	}
}

func TestPostDetailHandlerNotFound(t *testing.T) {
	setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/post/non-existent-slug", nil)
	rr := httptest.NewRecorder()
	postDetailHandler(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestPostPageHandler(t *testing.T) {
	setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/post/high-performance-sqlite-go-services", nil)
	rr := httptest.NewRecorder()
	postPageHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %q", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "High-Performance SQLite Microservices in Go") {
		t.Fatalf("body does not contain post title: %q", rr.Body.String())
	}
}

func TestAdminLoginAndSession(t *testing.T) {
	setupTestApp(t)

	form := url.Values{
		"username": {"admin"},
		"password": {"wrongpassword"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	loginHandler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("invalid password status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	validForm := url.Values{
		"username": {"admin"},
		"password": {"admin123"},
	}
	reqValid := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(validForm.Encode()))
	reqValid.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rrValid := httptest.NewRecorder()
	loginHandler(rrValid, reqValid)

	if rrValid.Code != http.StatusOK {
		t.Fatalf("valid login status = %d, want %d", rrValid.Code, http.StatusOK)
	}

	cookies := rrValid.Result().Cookies()
	var sessionToken string
	for _, c := range cookies {
		if c.Name == "session_token" {
			sessionToken = c.Value
		}
	}
	if sessionToken == "" {
		t.Fatal("session_token cookie not set")
	}

	username, ok := validateSession(db, sessionToken)
	if !ok || username != "admin" {
		t.Fatalf("validateSession = (%q, %v), want (admin, true)", username, ok)
	}
}

func TestAdminCreatePost(t *testing.T) {
	db := setupTestApp(t)
	token, err := createSession(db, "admin")
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}

	form := url.Values{
		"title":   {"Test Article Title"},
		"slug":    {"test-article-title"},
		"summary": {"Summary of test article"},
		"content": {"Paragraph 1 of content.\n\nParagraph 2 of content."},
		"tags":    {"GO, TESTING"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/posts", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	rr := httptest.NewRecorder()

	adminCreatePostHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("adminCreatePostHandler status = %d, want %d; body: %q", rr.Code, http.StatusOK, rr.Body.String())
	}

	post, err := getPostBySlug(db, "test-article-title")
	if err != nil {
		t.Fatalf("getPostBySlug created post: %v", err)
	}
	if post.Title != "Test Article Title" {
		t.Fatalf("created post title = %q, want Test Article Title", post.Title)
	}
}

func TestConfigUsesDefaultsForBlankValues(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_PATH", " ")
	t.Setenv("APP_ENV", "")
	t.Setenv("LOG_LEVEL", "")

	config := LoadConfig()
	if config.Port != "8080" {
		t.Fatalf("port = %q, want 8080", config.Port)
	}
	if config.DatabasePath != "/data/site.db" {
		t.Fatalf("database path = %q, want /data/site.db", config.DatabasePath)
	}
	if config.AppEnv != "development" {
		t.Fatalf("app env = %q, want development", config.AppEnv)
	}
	if config.LogLevel != "info" {
		t.Fatalf("log level = %q, want info", config.LogLevel)
	}
}

func TestStaticCacheMiddlewareSupportsETag(t *testing.T) {
	etags, err := buildAssetETags()
	if err != nil {
		t.Fatalf("build asset etags: %v", err)
	}
	handler := cacheMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte("body{}"))
	}), etags)

	first := httptest.NewRecorder()
	firstRequest := httptest.NewRequest(http.MethodGet, "/static/css/style.css", nil)
	handler.ServeHTTP(first, firstRequest)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("static response did not include an ETag")
	}

	second := httptest.NewRecorder()
	secondRequest := httptest.NewRequest(http.MethodGet, "/static/css/style.css", nil)
	secondRequest.Header.Set("If-None-Match", etag)
	handler.ServeHTTP(second, secondRequest)
	if second.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want %d", second.Code, http.StatusNotModified)
	}
}

func TestCompressionMiddlewareGzipsTextResponses(t *testing.T) {
	handler := compressionMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("compressible response"))
	}))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	handler.ServeHTTP(response, request)

	if response.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("content encoding = %q, want gzip", response.Header().Get("Content-Encoding"))
	}
	reader, err := gzip.NewReader(bytes.NewReader(response.Body.Bytes()))
	if err != nil {
		t.Fatalf("create gzip reader: %v", err)
	}
	decompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read compressed response: %v", err)
	}
	_ = reader.Close()
	if string(decompressed) != "compressible response" {
		t.Fatalf("decompressed response = %q", decompressed)
	}
}

func TestAdminDashboardUnauthorized(t *testing.T) {
	setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
	rr := httptest.NewRecorder()
	adminDashboardHandler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestAdminDashboardAuthorized(t *testing.T) {
	db := setupTestApp(t)
	token, err := createSession(db, "admin")
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	rr := httptest.NewRecorder()
	adminDashboardHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %q", rr.Code, http.StatusOK, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "MANAGED ARTICLES") {
		t.Fatalf("dashboard body missing MANAGED ARTICLES: %q", rr.Body.String())
	}
}

func TestAdminEditAndUpdatePost(t *testing.T) {
	db := setupTestApp(t)
	token, err := createSession(db, "admin")
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}

	posts, err := getPosts(db)
	if err != nil || len(posts) == 0 {
		t.Fatalf("getPosts error or empty: %v", err)
	}
	postID := posts[0].ID

	// Edit form endpoint
	reqEdit := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/admin/posts/edit/%d", postID), nil)
	reqEdit.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	rrEdit := httptest.NewRecorder()
	adminEditPostFormHandler(rrEdit, reqEdit)

	if rrEdit.Code != http.StatusOK {
		t.Fatalf("adminEditPostFormHandler status = %d, want %d", rrEdit.Code, http.StatusOK)
	}

	// Update endpoint
	form := url.Values{
		"title":   {"Updated Post Title"},
		"slug":    {posts[0].Slug},
		"summary": {"Updated summary text"},
		"content": {"Updated body text."},
		"tags":    {"UPDATED, GO"},
	}
	reqUpdate := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/posts/update/%d", postID), strings.NewReader(form.Encode()))
	reqUpdate.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqUpdate.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	rrUpdate := httptest.NewRecorder()
	adminUpdatePostHandler(rrUpdate, reqUpdate)

	if rrUpdate.Code != http.StatusOK {
		t.Fatalf("adminUpdatePostHandler status = %d, want %d; body: %q", rrUpdate.Code, http.StatusOK, rrUpdate.Body.String())
	}

	updated, err := getPostBySlug(db, posts[0].Slug)
	if err != nil {
		t.Fatalf("getPostBySlug: %v", err)
	}
	if updated.Title != "Updated Post Title" {
		t.Fatalf("updated title = %q, want Updated Post Title", updated.Title)
	}
}

func TestAdminDeletePost(t *testing.T) {
	db := setupTestApp(t)
	token, err := createSession(db, "admin")
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}

	err = createPost(db, Post{
		Slug:    "post-to-delete",
		Title:   "Post To Delete",
		Summary: "Summary",
		Content: "Content",
	})
	if err != nil {
		t.Fatalf("createPost: %v", err)
	}

	p, err := getPostBySlug(db, "post-to-delete")
	if err != nil {
		t.Fatalf("getPostBySlug: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/posts/delete/%d", p.ID), nil)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	rr := httptest.NewRecorder()
	adminDeletePostHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("adminDeletePostHandler status = %d, want %d", rr.Code, http.StatusOK)
	}

	_, err = getPostBySlug(db, "post-to-delete")
	if err == nil {
		t.Fatal("expected post to be deleted, but found")
	}
}

func TestAdminMarkAndDeleteContactMessage(t *testing.T) {
	db := setupTestApp(t)
	token, err := createSession(db, "admin")
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}

	// Insert test message
	_, err = db.Exec("INSERT INTO contact_messages (name, email, message) VALUES ('John Doe', 'john@example.com', 'Test message body')")
	if err != nil {
		t.Fatalf("insert contact_message: %v", err)
	}

	msgs, err := getContactMessages(db)
	if err != nil || len(msgs) == 0 {
		t.Fatalf("getContactMessages: %v", err)
	}
	msgID := msgs[0].ID

	// Mark read
	reqRead := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/messages/read/%d", msgID), nil)
	reqRead.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	rrRead := httptest.NewRecorder()
	adminMarkMessageReadHandler(rrRead, reqRead)

	if rrRead.Code != http.StatusOK {
		t.Fatalf("adminMarkMessageReadHandler status = %d, want %d", rrRead.Code, http.StatusOK)
	}

	msgsAfterRead, _ := getContactMessages(db)
	if len(msgsAfterRead) > 0 && !msgsAfterRead[0].IsRead {
		t.Fatalf("expected message to be read, got is_read = false")
	}

	// Delete message
	reqDel := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/messages/delete/%d", msgID), nil)
	reqDel.AddCookie(&http.Cookie{Name: "session_token", Value: token})
	rrDel := httptest.NewRecorder()
	adminDeleteMessageHandler(rrDel, reqDel)

	if rrDel.Code != http.StatusOK {
		t.Fatalf("adminDeleteMessageHandler status = %d, want %d", rrDel.Code, http.StatusOK)
	}

	msgsAfterDel, _ := getContactMessages(db)
	if len(msgsAfterDel) != 0 {
		t.Fatalf("expected 0 messages after delete, got %d", len(msgsAfterDel))
	}
}
