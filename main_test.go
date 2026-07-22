package main

import (
	"bytes"
	"compress/gzip"
	"database/sql"
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

	templates = template.Must(template.ParseFS(embeddedFS, "templates/*.html", "templates/partials/*.html"))
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
	if count != 1 {
		t.Fatalf("schema migrations count = %d, want 1", count)
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
