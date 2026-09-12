package main

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
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
	loginLimiter.reset()
	contactLimiter.reset()

	return db
}

func createTestSession(t *testing.T, db *sql.DB) (string, string) {
	t.Helper()

	if _, err := db.Exec("INSERT OR IGNORE INTO admin_users (username, password_hash) VALUES ('admin', 'test-fixture')"); err != nil {
		t.Fatalf("create test admin: %v", err)
	}

	token, csrfToken, err := createSession(db, "admin")
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}
	return token, csrfToken
}

func addMutationHeaders(req *http.Request, token, csrfToken string) {
	req.Header.Set("Origin", "http://example.com")
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
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
	if count != 5 {
		t.Fatalf("schema migrations count = %d, want 5", count)
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
	db := setupTestApp(t)

	const adminPassword = "correct-horse-battery-staple"
	if err := setAdminPassword(db, "admin", adminPassword); err != nil {
		t.Fatalf("setAdminPassword: %v", err)
	}

	form := url.Values{
		"username": {"admin"},
		"password": {"wrongpassword"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	rr := httptest.NewRecorder()
	loginHandler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("invalid password status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	validForm := url.Values{
		"username": {"admin"},
		"password": {adminPassword},
	}
	reqValid := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(validForm.Encode()))
	reqValid.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqValid.Header.Set("Origin", "http://example.com")
	rrValid := httptest.NewRecorder()
	loginHandler(rrValid, reqValid)

	if rrValid.Code != http.StatusOK {
		t.Fatalf("valid login status = %d, want %d", rrValid.Code, http.StatusOK)
	}

	cookies := rrValid.Result().Cookies()
	var sessionToken, csrfToken string
	for _, c := range cookies {
		switch c.Name {
		case "session_token":
			sessionToken = c.Value
			if !c.HttpOnly {
				t.Fatal("session_token cookie must be HttpOnly")
			}
		case "csrf_token":
			csrfToken = c.Value
		}
	}
	if sessionToken == "" {
		t.Fatal("session_token cookie not set")
	}
	if csrfToken == "" {
		t.Fatal("csrf_token cookie not set")
	}

	username, ok := validateSession(db, sessionToken)
	if !ok || username != "admin" {
		t.Fatalf("validateSession = (%q, %v), want (admin, true)", username, ok)
	}
}

func TestAdminCreatePost(t *testing.T) {
	db := setupTestApp(t)
	token, csrfToken := createTestSession(t, db)

	form := url.Values{
		"title":   {"Test Article Title"},
		"slug":    {"test-article-title"},
		"summary": {"Summary of test article"},
		"content": {"Paragraph 1 of content.\n\nParagraph 2 of content."},
		"tags":    {"GO, TESTING"},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/posts", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addMutationHeaders(req, token, csrfToken)
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
	token, _ := createTestSession(t, db)

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
	token, csrfToken := createTestSession(t, db)

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
	addMutationHeaders(reqUpdate, token, csrfToken)
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
	token, csrfToken := createTestSession(t, db)

	err := createPost(db, Post{
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
	addMutationHeaders(req, token, csrfToken)
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
	token, csrfToken := createTestSession(t, db)

	// Insert test message
	_, err := db.Exec("INSERT INTO contact_messages (name, email, message) VALUES ('John Doe', 'john@example.com', 'Test message body')")
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
	addMutationHeaders(reqRead, token, csrfToken)
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
	addMutationHeaders(reqDel, token, csrfToken)
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

func TestSetAdminPasswordRejectsWeakPassword(t *testing.T) {
	db := setupTestApp(t)

	if err := setAdminPassword(db, "admin", "short"); err == nil {
		t.Fatal("expected short password to be rejected")
	}
	if err := setAdminPassword(db, "admin", strings.Repeat("a", maxAdminPasswordLength+1)); err == nil {
		t.Fatal("expected oversized password to be rejected")
	}
}

func TestAuthenticateAdminAcceptsArgon2idPassword(t *testing.T) {
	db := setupTestApp(t)

	const adminPassword = "a-very-secure-password"
	if err := setAdminPassword(db, "admin", adminPassword); err != nil {
		t.Fatalf("setAdminPassword: %v", err)
	}

	if !authenticateAdmin(db, "admin", adminPassword) {
		t.Fatal("expected valid password to authenticate")
	}
	if authenticateAdmin(db, "admin", "wrong-password") {
		t.Fatal("expected wrong password to be rejected")
	}
	if authenticateAdmin(db, "missing", adminPassword) {
		t.Fatal("expected unknown user to be rejected")
	}
}

func TestAuthenticateAdminRejectsLegacyHash(t *testing.T) {
	db := setupTestApp(t)

	legacyHash := "static_portfolio_salt:d34db33f"
	if _, err := db.Exec("INSERT INTO admin_users (username, password_hash) VALUES (?, ?)", "legacy", legacyHash); err != nil {
		t.Fatalf("insert legacy admin: %v", err)
	}

	if authenticateAdmin(db, "legacy", "admin123") {
		t.Fatal("legacy SHA-256 hash must not authenticate")
	}
}

func TestSetAdminPasswordInvalidatesSessions(t *testing.T) {
	db := setupTestApp(t)

	if err := setAdminPassword(db, "admin", "first-secure-password"); err != nil {
		t.Fatalf("setAdminPassword: %v", err)
	}

	token, _, err := createSession(db, "admin")
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}

	if err := setAdminPassword(db, "admin", "second-secure-password"); err != nil {
		t.Fatalf("setAdminPassword: %v", err)
	}

	if _, ok := validateSession(db, token); ok {
		t.Fatal("expected password change to invalidate existing sessions")
	}
}

func TestHasSecureAdmin(t *testing.T) {
	db := setupTestApp(t)

	secure, err := hasSecureAdmin(db)
	if err != nil {
		t.Fatalf("hasSecureAdmin: %v", err)
	}
	if secure {
		t.Fatal("expected fresh database to have no secure admin")
	}

	if err := setAdminPassword(db, "admin", "a-very-secure-password"); err != nil {
		t.Fatalf("setAdminPassword: %v", err)
	}

	secure, err = hasSecureAdmin(db)
	if err != nil {
		t.Fatalf("hasSecureAdmin: %v", err)
	}
	if !secure {
		t.Fatal("expected secure admin after setAdminPassword")
	}
}

func TestAdminSetPasswordCommandFromStdin(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "admin-command.db")
	t.Setenv("DATABASE_PATH", databasePath)

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	oldStdin := os.Stdin
	os.Stdin = readEnd
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = readEnd.Close()
	})

	if _, err := writeEnd.WriteString("command-line-password\n"); err != nil {
		t.Fatalf("write password to pipe: %v", err)
	}
	if err := writeEnd.Close(); err != nil {
		t.Fatalf("close password pipe: %v", err)
	}

	if err := runAdminCommand([]string{"set-password", "--password-stdin"}); err != nil {
		t.Fatalf("runAdminCommand: %v", err)
	}

	commandDB, err := openDB(databasePath)
	if err != nil {
		t.Fatalf("open command database: %v", err)
	}
	t.Cleanup(func() { _ = commandDB.Close() })

	if !authenticateAdmin(commandDB, "admin", "command-line-password") {
		t.Fatal("expected password set through the command to authenticate")
	}

	var legacyCount int
	if err := commandDB.QueryRow("SELECT COUNT(*) FROM admin_users WHERE password_hash NOT LIKE '$argon2id$%'").Scan(&legacyCount); err != nil {
		t.Fatalf("count legacy hashes: %v", err)
	}
	if legacyCount != 0 {
		t.Fatalf("legacy hash count = %d, want 0", legacyCount)
	}
}

func TestFreshDatabaseHasNoAdminUsers(t *testing.T) {
	db := setupTestApp(t)

	if got := countRows(t, db, "SELECT COUNT(*) FROM admin_users"); got != 0 {
		t.Fatalf("admin_users count = %d, want 0", got)
	}
}

func TestLoginKeepsPasswordSpaces(t *testing.T) {
	db := setupTestApp(t)

	const adminPassword = "  spaced password  "
	if err := setAdminPassword(db, "admin", adminPassword); err != nil {
		t.Fatalf("setAdminPassword: %v", err)
	}

	login := func(password string) int {
		form := url.Values{"username": {"admin"}, "password": {password}}
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.com")
		rr := httptest.NewRecorder()
		loginHandler(rr, req)
		return rr.Code
	}

	if code := login(adminPassword); code != http.StatusOK {
		t.Fatalf("exact password status = %d, want %d", code, http.StatusOK)
	}
	if code := login(strings.TrimSpace(adminPassword)); code != http.StatusUnauthorized {
		t.Fatalf("trimmed password status = %d, want %d", code, http.StatusUnauthorized)
	}
}

func TestLoginRequiresSameOrigin(t *testing.T) {
	db := setupTestApp(t)

	if err := setAdminPassword(db, "admin", "a-very-secure-password"); err != nil {
		t.Fatalf("setAdminPassword: %v", err)
	}

	form := url.Values{"username": {"admin"}, "password": {"a-very-secure-password"}}
	for _, origin := range []string{"", "https://evil.example.com"} {
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rr := httptest.NewRecorder()
		loginHandler(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Fatalf("origin %q status = %d, want %d", origin, rr.Code, http.StatusForbidden)
		}
	}
}

func TestLoginRateLimited(t *testing.T) {
	db := setupTestApp(t)

	if err := setAdminPassword(db, "admin", "a-very-secure-password"); err != nil {
		t.Fatalf("setAdminPassword: %v", err)
	}

	form := url.Values{"username": {"admin"}, "password": {"wrong-password"}}
	for attempt := 0; attempt < 5; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.com")
		rr := httptest.NewRecorder()
		loginHandler(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want %d", attempt, rr.Code, http.StatusUnauthorized)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "http://example.com")
	rr := httptest.NewRecorder()
	loginHandler(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limited status = %d, want %d", rr.Code, http.StatusTooManyRequests)
	}
}

func TestContactRateLimited(t *testing.T) {
	setupTestApp(t)

	form := url.Values{
		"name":    {"Review"},
		"email":   {"review@example.com"},
		"message": {"hello"},
	}
	for attempt := 0; attempt < 5; attempt++ {
		if rr := postContact(t, form); rr.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d, want %d", attempt, rr.Code, http.StatusOK)
		}
	}
	if rr := postContact(t, form); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limited status = %d, want %d", rr.Code, http.StatusTooManyRequests)
	}
}

func TestClientIPResolvesForwardedClientBehindLocalProxy(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		forwarded  []string
		want       string
	}{
		{name: "private peer uses forwarded", remoteAddr: "10.0.0.5:4321", forwarded: []string{"203.0.113.7"}, want: "203.0.113.7"},
		{name: "multiple entries use rightmost", remoteAddr: "172.17.0.2:4321", forwarded: []string{"198.51.100.9, 203.0.113.7"}, want: "203.0.113.7"},
		{name: "loopback peer strips forwarded port", remoteAddr: "127.0.0.1:4321", forwarded: []string{"203.0.113.7:5555"}, want: "203.0.113.7"},
		{name: "public peer ignores forwarded", remoteAddr: "203.0.113.50:4321", forwarded: []string{"10.0.0.1"}, want: "203.0.113.50"},
		{name: "private peer without forwarded", remoteAddr: "10.0.0.5:4321", want: "10.0.0.5"},
		{name: "invalid forwarded falls back to peer", remoteAddr: "10.0.0.5:4321", forwarded: []string{"not-an-ip"}, want: "10.0.0.5"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/login", nil)
			req.RemoteAddr = tc.remoteAddr
			for _, value := range tc.forwarded {
				req.Header.Add("X-Forwarded-For", value)
			}
			if got := clientIP(req); got != tc.want {
				t.Fatalf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoginRateLimitSeparatesProxyClients(t *testing.T) {
	db := setupTestApp(t)

	if err := setAdminPassword(db, "admin", "a-very-secure-password"); err != nil {
		t.Fatalf("setAdminPassword: %v", err)
	}

	form := url.Values{"username": {"admin"}, "password": {"wrong-password"}}
	attempt := func(client string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.com")
		req.RemoteAddr = "10.0.0.5:4321"
		req.Header.Set("X-Forwarded-For", client)
		rr := httptest.NewRecorder()
		loginHandler(rr, req)
		return rr.Code
	}

	for attemptNum := 0; attemptNum < 5; attemptNum++ {
		if code := attempt("203.0.113.7"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want %d", attemptNum, code, http.StatusUnauthorized)
		}
	}
	if code := attempt("203.0.113.7"); code != http.StatusTooManyRequests {
		t.Fatalf("same client status = %d, want %d", code, http.StatusTooManyRequests)
	}
	if code := attempt("203.0.113.8"); code != http.StatusUnauthorized {
		t.Fatalf("different client status = %d, want %d", code, http.StatusUnauthorized)
	}
}

func TestSessionTokenIsHashedAtRest(t *testing.T) {
	db := setupTestApp(t)

	if err := setAdminPassword(db, "admin", "a-very-secure-password"); err != nil {
		t.Fatalf("setAdminPassword: %v", err)
	}

	token, csrfToken, err := createSession(db, "admin")
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}

	var storedID, storedCSRF, expiresAt string
	if err := db.QueryRow("SELECT id, csrf_hash, expires_at FROM sessions LIMIT 1").Scan(&storedID, &storedCSRF, &expiresAt); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if storedID == token {
		t.Fatal("raw session token must not be stored")
	}
	if storedID != hashSessionToken(token) {
		t.Fatalf("stored session id = %q, want token hash", storedID)
	}
	if storedCSRF == csrfToken || storedCSRF != hashSessionToken(csrfToken) {
		t.Fatal("csrf token must be stored only as a hash")
	}
	if _, err := time.Parse(time.RFC3339, expiresAt); err != nil {
		t.Fatalf("expires_at is not RFC3339: %v", err)
	}
}

func TestSessionRequiresExistingAdmin(t *testing.T) {
	db := setupTestApp(t)

	if err := setAdminPassword(db, "admin", "a-very-secure-password"); err != nil {
		t.Fatalf("setAdminPassword: %v", err)
	}
	token, _, err := createSession(db, "admin")
	if err != nil {
		t.Fatalf("createSession: %v", err)
	}

	if _, err := db.Exec("DELETE FROM admin_users"); err != nil {
		t.Fatalf("delete admin users: %v", err)
	}

	if _, ok := validateSession(db, token); ok {
		t.Fatal("session must be invalid when the admin user no longer exists")
	}
}

func TestExpiredSessionsAreRemovedOnCreate(t *testing.T) {
	db := setupTestApp(t)

	if err := setAdminPassword(db, "admin", "a-very-secure-password"); err != nil {
		t.Fatalf("setAdminPassword: %v", err)
	}

	expiredAt := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if _, err := db.Exec("INSERT INTO sessions (id, username, expires_at, csrf_hash) VALUES (?, ?, ?, ?)",
		"expired-session", "admin", expiredAt, "expired-csrf"); err != nil {
		t.Fatalf("insert expired session: %v", err)
	}

	if _, _, err := createSession(db, "admin"); err != nil {
		t.Fatalf("createSession: %v", err)
	}

	if got := countRows(t, db, "SELECT COUNT(*) FROM sessions WHERE id = 'expired-session'"); got != 0 {
		t.Fatalf("expired sessions count = %d, want 0", got)
	}
}

func TestAdminMutationRequiresCSRFAndOrigin(t *testing.T) {
	db := setupTestApp(t)
	token, csrfToken := createTestSession(t, db)

	cases := []struct {
		name   string
		origin string
		csrf   string
	}{
		{name: "missing csrf", origin: "http://example.com", csrf: ""},
		{name: "wrong csrf", origin: "http://example.com", csrf: "not-the-token"},
		{name: "cross origin", origin: "https://evil.example.com", csrf: csrfToken},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/admin/posts/delete/1", nil)
			req.Header.Set("Origin", tc.origin)
			if tc.csrf != "" {
				req.Header.Set("X-CSRF-Token", tc.csrf)
			}
			req.AddCookie(&http.Cookie{Name: "session_token", Value: token})
			rr := httptest.NewRecorder()
			adminDeletePostHandler(rr, req)

			if rr.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusForbidden)
			}
		})
	}
}

func TestAdminCreatePostValidatesInput(t *testing.T) {
	db := setupTestApp(t)
	token, csrfToken := createTestSession(t, db)

	cases := []struct {
		name string
		form url.Values
	}{
		{
			name: "invalid slug",
			form: url.Values{"title": {"Title"}, "slug": {"Invalid Slug!"}, "summary": {"s"}, "content": {"c"}},
		},
		{
			name: "title too long",
			form: url.Values{"title": {strings.Repeat("a", maxPostTitleLen+1)}, "summary": {"s"}, "content": {"c"}},
		},
		{
			name: "too many tags",
			form: url.Values{"title": {"Title"}, "summary": {"s"}, "content": {"c"}, "tags": {"a,b,c,d,e,f,g,h,i,j,k"}},
		},
		{
			name: "oversized body",
			form: url.Values{"title": {"Title"}, "summary": {"s"}, "content": {strings.Repeat("a", maxPostBodyBytes+100)}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/admin/posts", strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			addMutationHeaders(req, token, csrfToken)
			rr := httptest.NewRecorder()
			adminCreatePostHandler(rr, req)

			if rr.Code != http.StatusUnprocessableEntity && rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 or 422; body: %q", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestParsePasswordHashRejectsOutOfRangeParameters(t *testing.T) {
	salt := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{1}, argon2IDSaltLength))
	key := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{2}, argon2IDKeyLength))

	cases := []string{
		fmt.Sprintf("$argon2id$v=%d$m=%d,t=3,p=2$%s$%s", argon2.Version, argon2IDMaxMemory+1, salt, key),
		fmt.Sprintf("$argon2id$v=%d$m=65536,t=%d,p=2$%s$%s", argon2.Version, argon2IDMaxIterations+1, salt, key),
		fmt.Sprintf("$argon2id$v=%d$m=65536,t=3,p=%d$%s$%s", argon2.Version, argon2IDMaxParallelism+1, salt, key),
		fmt.Sprintf("$argon2id$v=%d$m=65536,t=3$%s$%s", argon2.Version, salt, key),
		fmt.Sprintf("$argon2id$v=%d$m=65536,t=3,p=2$%s$%s", argon2.Version+1, salt, key),
	}

	for _, encoded := range cases {
		if _, _, _, err := parsePasswordHash(encoded); err == nil {
			t.Fatalf("expected parse error for %q", encoded)
		}
	}
}

func TestHasSecureAdminRejectsAnyInvalidHash(t *testing.T) {
	db := setupTestApp(t)

	if err := setAdminPassword(db, "admin", "a-very-secure-password"); err != nil {
		t.Fatalf("setAdminPassword: %v", err)
	}
	if _, err := db.Exec("INSERT INTO admin_users (username, password_hash) VALUES (?, ?)", "legacy", "salt:deadbeef"); err != nil {
		t.Fatalf("insert legacy admin: %v", err)
	}

	secure, err := hasSecureAdmin(db)
	if err != nil {
		t.Fatalf("hasSecureAdmin: %v", err)
	}
	if secure {
		t.Fatal("any invalid hash must make hasSecureAdmin false")
	}
}

func TestTemplatesUseEmbeddedAssets(t *testing.T) {
	setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	indexHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("index status = %d, want %d", rr.Code, http.StatusOK)
	}

	body := rr.Body.String()
	for _, forbidden := range []string{"unpkg.com", "cdn.jsdelivr.net", "fonts.googleapis.com", "fonts.gstatic.com"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("index still references external asset %s", forbidden)
		}
	}
	for _, required := range []string{
		"/static/js/htmx.min.js",
		"/static/js/alpine.min.js",
		"/static/js/app.js",
		"/static/css/fonts.css",
		"/static/css/phosphor.css",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("index missing %s", required)
		}
	}
}

func TestEmbeddedAssetsIncludeVendoredFrontend(t *testing.T) {
	etags, err := buildAssetETags()
	if err != nil {
		t.Fatalf("buildAssetETags: %v", err)
	}

	for _, asset := range []string{
		"/static/js/htmx.min.js",
		"/static/js/alpine.min.js",
		"/static/js/app.js",
		"/static/css/fonts.css",
		"/static/css/phosphor.css",
		"/static/fonts/Phosphor.woff2",
	} {
		if etags[asset] == "" {
			t.Fatalf("missing embedded asset %s", asset)
		}
	}
}

func TestContentSecurityPolicyIsSelfHosted(t *testing.T) {
	handler := standardMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	csp := rr.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("missing Content-Security-Policy header")
	}
	for _, forbidden := range []string{"unpkg.com", "jsdelivr", "fonts.googleapis.com", "fonts.gstatic.com"} {
		if strings.Contains(csp, forbidden) {
			t.Fatalf("CSP still allows %s: %s", forbidden, csp)
		}
	}
	if !strings.Contains(csp, "script-src 'self' 'unsafe-eval'") {
		t.Fatalf("unexpected script-src in CSP: %s", csp)
	}
}
