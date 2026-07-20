package main

import (
	"database/sql"
	"html/template"
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
	db, err = initDB(filepath.Join(t.TempDir(), "portfolio_test.db"))
	if err != nil {
		t.Fatalf("initDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	templates = template.Must(template.ParseFS(templateFS, "templates/*.html", "templates/partials/*.html"))

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
