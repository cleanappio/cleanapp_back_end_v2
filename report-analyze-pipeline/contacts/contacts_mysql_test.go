package contacts

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Run only against an empty dedicated local fixture, never the application DB.
func TestContactRoutingMySQLFixture(t *testing.T) {
	dsn := os.Getenv("CONTACTS_TEST_DSN")
	if dsn == "" {
		t.Skip("set CONTACTS_TEST_DSN to a dedicated local database ending _contact_routing_test")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.Net != "tcp" || !(strings.HasPrefix(cfg.Addr, "127.0.0.1:") || strings.HasPrefix(cfg.Addr, "localhost:")) || !strings.HasSuffix(cfg.DBName, "_contact_routing_test") {
		t.Fatal("fixture only permits dedicated local databases ending _contact_routing_test")
	}
	cfg.ParseTime = true
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 5*time.Second, 5*time.Second, 5*time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal("cannot open local contact fixture")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `CREATE TABLE brand_contacts (
		id INT AUTO_INCREMENT PRIMARY KEY,brand_name VARCHAR(255) NOT NULL,product_name VARCHAR(255),
		contact_name VARCHAR(255),contact_title VARCHAR(255),contact_level VARCHAR(32) DEFAULT 'ic',
		email VARCHAR(255),email_verified BOOLEAN DEFAULT FALSE,twitter_handle VARCHAR(255),
		linkedin_url VARCHAR(512),github_handle VARCHAR(255),source VARCHAR(32) DEFAULT 'manual',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		UNIQUE KEY(brand_name,email)) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`); err != nil {
		t.Fatal("fixture requires an empty dedicated local contact database")
	}
	defer db.Exec(`DROP TABLE brand_contacts`)
	if _, err := db.ExecContext(ctx, `INSERT INTO brand_contacts
		(brand_name,product_name,contact_name,contact_title,contact_level,email,email_verified,source) VALUES
		('example','product-a','Curated administrator','Support','manager','curated@example.org',TRUE,'manual'),
		('example',NULL,'Historic extracted address','Reported via App','ic','legacy@example.org',TRUE,'manual'),
		('example','product-a','Mixed case marker','  REPORTED VIA APP  ','ic','case-marker@example.org',TRUE,'manual'),
		('example',NULL,'Admin without a title',NULL,'ic','untitled@example.org',TRUE,'manual'),
		('example','product-b','Other product support','Support','manager','product-b@example.org',TRUE,'manual')`); err != nil {
		t.Fatal(err)
	}
	service := NewContactService(db)
	if err := service.ProcessReportDescription("example", "The report quotes new-suggestion@example.org, curated@example.org and @unverified_handle"); err != nil {
		t.Fatal(err)
	}
	var verified bool
	var source, title string
	if err := db.QueryRowContext(ctx, `SELECT email_verified,source,contact_title FROM brand_contacts WHERE email='new-suggestion@example.org'`).Scan(&verified, &source, &title); err != nil || verified || source != "manual" || title != "Reported via App" {
		t.Fatalf("new suggestion gained verification or lost quarantine marker: verified=%v source=%q title=%q err=%v", verified, source, title, err)
	}
	// Exercise the race-safe duplicate path directly: the curated record must not
	// be downgraded or acquire the report-extraction marker on a repeated address.
	if err := service.saveReportSuggestion("example", "curated@example.org", "@untrusted"); err != nil {
		t.Fatal(err)
	}
	var name, handle string
	if err := db.QueryRowContext(ctx, `SELECT contact_name,contact_title,email_verified,COALESCE(twitter_handle,'') FROM brand_contacts WHERE email='curated@example.org'`).Scan(&name, &title, &verified, &handle); err != nil || name != "Curated administrator" || title != "Support" || !verified || handle != "" {
		t.Fatalf("report text overwrote a curated contact: name=%q title=%q verified=%v handle=%q err=%v", name, title, verified, handle, err)
	}
	checkEmails := func(contacts []Contact, want []string) {
		t.Helper()
		var got []string
		for _, contact := range contacts {
			got = append(got, contact.Email)
		}
		// Order among equal seniorities is intentionally unspecified.
		if len(got) != len(want) {
			t.Fatalf("wrong routable contacts: got %v want %v", got, want)
		}
		for _, email := range want {
			found := false
			for _, actual := range got {
				found = found || email == actual
			}
			if !found {
				t.Fatalf("missing legitimate administrator contact %q in %v", email, got)
			}
		}
	}
	brand, err := service.GetContactsForBrand("example")
	if err != nil {
		t.Fatal(err)
	}
	checkEmails(brand, []string{"curated@example.org", "untitled@example.org", "product-b@example.org"})
	product, err := service.GetContactsForBrandProduct("example", "product-a")
	if err != nil {
		t.Fatal(err)
	}
	checkEmails(product, []string{"curated@example.org", "untitled@example.org"})
	var retained []string
	rows, err := db.QueryContext(ctx, `SELECT email FROM brand_contacts WHERE LOWER(TRIM(contact_title))='reported via app' AND source='manual' ORDER BY email`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			t.Fatal(err)
		}
		retained = append(retained, email)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(retained, []string{"", "case-marker@example.org", "legacy@example.org", "new-suggestion@example.org"}) {
		t.Fatalf("suggestions were deleted or not retained: %v", retained)
	}
}
