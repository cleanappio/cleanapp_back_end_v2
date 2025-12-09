package contacts

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Contact represents a person who can be contacted about a brand/product
type Contact struct {
	ID            int64     `json:"id"`
	BrandName     string    `json:"brand_name"`
	ProductName   string    `json:"product_name,omitempty"`
	ContactName   string    `json:"contact_name"`
	ContactTitle  string    `json:"contact_title,omitempty"`
	ContactLevel  string    `json:"contact_level"` // ic, manager, director, vp, c_suite, founder
	Email         string    `json:"email,omitempty"`
	EmailVerified bool      `json:"email_verified"`
	TwitterHandle string    `json:"twitter_handle,omitempty"`
	LinkedInURL   string    `json:"linkedin_url,omitempty"`
	GitHubHandle  string    `json:"github_handle,omitempty"`
	Source        string    `json:"source"` // linkedin, website, github, manual, inferred
	CreatedAt     time.Time `json:"created_at"`
}

// ContactService handles contact discovery and storage
type ContactService struct {
	db         *sql.DB
	httpClient *http.Client
}

// NewContactService creates a new contact service
func NewContactService(db *sql.DB) *ContactService {
	return &ContactService{
		db: db,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// CreateBrandContactsTable creates the brand_contacts table if it doesn't exist
func (s *ContactService) CreateBrandContactsTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS brand_contacts (
		id INT AUTO_INCREMENT PRIMARY KEY,
		brand_name VARCHAR(255) NOT NULL,
		product_name VARCHAR(255),
		contact_name VARCHAR(255),
		contact_title VARCHAR(255),
		contact_level ENUM('ic', 'manager', 'director', 'vp', 'c_suite', 'founder') DEFAULT 'ic',
		email VARCHAR(255),
		email_verified BOOLEAN DEFAULT FALSE,
		twitter_handle VARCHAR(255),
		linkedin_url VARCHAR(512),
		github_handle VARCHAR(255),
		source ENUM('linkedin', 'website', 'github', 'twitter', 'manual', 'inferred') DEFAULT 'manual',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		INDEX idx_brand_name (brand_name),
		INDEX idx_brand_product (brand_name, product_name),
		INDEX idx_contact_level (contact_level),
		UNIQUE KEY uk_brand_contact_email (brand_name, email)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`
	_, err := s.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create brand_contacts table: %w", err)
	}
	log.Println("brand_contacts table verified/created")
	return nil
}

// GetContactsForBrand retrieves all contacts for a brand
func (s *ContactService) GetContactsForBrand(brandName string) ([]Contact, error) {
	query := `
	SELECT id, brand_name, product_name, contact_name, contact_title, contact_level,
	       email, email_verified, twitter_handle, linkedin_url, github_handle, source, created_at
	FROM brand_contacts
	WHERE brand_name = ?
	ORDER BY CASE WHEN contact_title = 'Reported via App' THEN 0 ELSE 1 END, FIELD(contact_level, 'founder', 'c_suite', 'vp', 'director', 'manager', 'ic')
	`
	
	rows, err := s.db.Query(query, brandName)
	if err != nil {
		return nil, fmt.Errorf("failed to query contacts for brand %s: %w", brandName, err)
	}
	defer rows.Close()
	
	var contacts []Contact
	for rows.Next() {
		var c Contact
		var productName, contactTitle, email, twitter, linkedin, github sql.NullString
		
		err := rows.Scan(
			&c.ID, &c.BrandName, &productName, &c.ContactName, &contactTitle, &c.ContactLevel,
			&email, &c.EmailVerified, &twitter, &linkedin, &github, &c.Source, &c.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan contact: %w", err)
		}
		
		c.ProductName = productName.String
		c.ContactTitle = contactTitle.String
		c.Email = email.String
		c.TwitterHandle = twitter.String
		c.LinkedInURL = linkedin.String
		c.GitHubHandle = github.String
		
		contacts = append(contacts, c)
	}
	
	return contacts, nil
}

// GetContactsForBrandProduct retrieves contacts for a specific product
func (s *ContactService) GetContactsForBrandProduct(brandName, productName string) ([]Contact, error) {
	query := `
	SELECT id, brand_name, product_name, contact_name, contact_title, contact_level,
	       email, email_verified, twitter_handle, linkedin_url, github_handle, source, created_at
	FROM brand_contacts
	WHERE brand_name = ? AND (product_name = ? OR product_name IS NULL OR product_name = '')
	ORDER BY CASE WHEN contact_title = 'Reported via App' THEN 0 ELSE 1 END, FIELD(contact_level, 'founder', 'c_suite', 'vp', 'director', 'manager', 'ic')
	`
	
	rows, err := s.db.Query(query, brandName, productName)
	if err != nil {
		return nil, fmt.Errorf("failed to query contacts for brand %s product %s: %w", brandName, productName, err)
	}
	defer rows.Close()
	
	var contacts []Contact
	for rows.Next() {
		var c Contact
		var productNameDB, contactTitle, email, twitter, linkedin, github sql.NullString
		
		err := rows.Scan(
			&c.ID, &c.BrandName, &productNameDB, &c.ContactName, &contactTitle, &c.ContactLevel,
			&email, &c.EmailVerified, &twitter, &linkedin, &github, &c.Source, &c.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan contact: %w", err)
		}
		
		c.ProductName = productNameDB.String
		c.ContactTitle = contactTitle.String
		c.Email = email.String
		c.TwitterHandle = twitter.String
		c.LinkedInURL = linkedin.String
		c.GitHubHandle = github.String
		
		contacts = append(contacts, c)
	}
	
	return contacts, nil
}

// SaveContact saves or updates a contact
func (s *ContactService) SaveContact(c *Contact) error {
	query := `
	INSERT INTO brand_contacts (
		brand_name, product_name, contact_name, contact_title, contact_level,
		email, email_verified, twitter_handle, linkedin_url, github_handle, source
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		contact_name = VALUES(contact_name),
		contact_title = VALUES(contact_title),
		contact_level = VALUES(contact_level),
		email_verified = VALUES(email_verified),
		twitter_handle = VALUES(twitter_handle),
		linkedin_url = VALUES(linkedin_url),
		github_handle = VALUES(github_handle),
		source = VALUES(source),
		updated_at = NOW()
	`
	
	_, err := s.db.Exec(query,
		c.BrandName, c.ProductName, c.ContactName, c.ContactTitle, c.ContactLevel,
		c.Email, c.EmailVerified, c.TwitterHandle, c.LinkedInURL, c.GitHubHandle, c.Source,
	)
	if err != nil {
		return fmt.Errorf("failed to save contact: %w", err)
	}
	
	return nil
}

// InferEmailsFromName generates possible email addresses for a name at a domain
func InferEmailsFromName(fullName, domain string) []string {
	if fullName == "" || domain == "" {
		return nil
	}
	
	// Normalize name
	name := strings.ToLower(strings.TrimSpace(fullName))
	parts := strings.Fields(name)
	
	if len(parts) == 0 {
		return nil
	}
	
	firstName := parts[0]
	var lastName string
	if len(parts) > 1 {
		lastName = parts[len(parts)-1]
	}
	
	var emails []string
	
	// Common patterns
	emails = append(emails, firstName+"@"+domain)                              // sam@openai.com
	if lastName != "" {
		emails = append(emails, firstName+lastName+"@"+domain)                 // samaltman@openai.com
		emails = append(emails, firstName+"."+lastName+"@"+domain)             // sam.altman@openai.com
		emails = append(emails, string(firstName[0])+lastName+"@"+domain)      // saltman@openai.com
		emails = append(emails, firstName+"_"+lastName+"@"+domain)             // sam_altman@openai.com
		emails = append(emails, lastName+"@"+domain)                           // altman@openai.com
	}
	
	return emails
}

// ValidateEmailDomain checks if a domain has valid MX records
func ValidateEmailDomain(email string) bool {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	// For now, just do basic syntax check
	// Could add MX lookup: net.LookupMX(parts[1])
	return true
}

// ScrapeContactsFromWebsite attempts to find contact emails from a company website
func (s *ContactService) ScrapeContactsFromWebsite(websiteURL string) ([]Contact, error) {
	if websiteURL == "" {
		return nil, nil
	}

	// Ensure URL has scheme
	if !strings.HasPrefix(websiteURL, "http://") && !strings.HasPrefix(websiteURL, "https://") {
		websiteURL = "https://" + websiteURL
	}

	// Try common about/team pages
	aboutPages := []string{
		websiteURL + "/about",
		websiteURL + "/team",
		websiteURL + "/about-us",
		websiteURL + "/company",
		websiteURL + "/leadership",
	}

	var contacts []Contact
	emailRegex := regexp.MustCompile(`mailto:([a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,})`)

	for _, pageURL := range aboutPages {
		req, err := http.NewRequest("GET", pageURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "CleanApp/1.0 (https://cleanapp.io)")

		resp, err := s.httpClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
		resp.Body.Close()
		if err != nil {
			continue
		}

		// Find emails
		matches := emailRegex.FindAllStringSubmatch(string(body), -1)
		for _, match := range matches {
			if len(match) > 1 {
				email := strings.ToLower(match[1])
				// Create a basic contact from scraped email
				contacts = append(contacts, Contact{
					Email:  email,
					Source: "website",
				})
			}
		}
	}

	return contacts, nil
}

// GetEmailsForBrand returns all emails for a brand, ordered by seniority
func (s *ContactService) GetEmailsForBrand(brandName string) ([]string, error) {
	contacts, err := s.GetContactsForBrand(brandName)
	if err != nil {
		return nil, err
	}
	
	var emails []string
	seen := make(map[string]bool)
	
	for _, c := range contacts {
		if c.Email != "" && !seen[strings.ToLower(c.Email)] {
			emails = append(emails, c.Email)
			seen[strings.ToLower(c.Email)] = true
		}
	}
	
	return emails, nil
}

// GetSocialHandlesForBrand returns Twitter handles for a brand
func (s *ContactService) GetSocialHandlesForBrand(brandName string) ([]string, error) {
	contacts, err := s.GetContactsForBrand(brandName)
	if err != nil {
		return nil, err
	}
	
	var handles []string
	seen := make(map[string]bool)
	
	for _, c := range contacts {
		if c.TwitterHandle != "" && !seen[strings.ToLower(c.TwitterHandle)] {
			handles = append(handles, c.TwitterHandle)
			seen[strings.ToLower(c.TwitterHandle)] = true
		}
	}
	
	return handles, nil
}
