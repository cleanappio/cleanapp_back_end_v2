package contacts

import (
	"log"
	"regexp"
	"strings"
)

// ExtractContactsFromText parses text for emails and social handles
func ExtractContactsFromText(text string) (emails []string, handles []string) {
	if text == "" {
		return nil, nil
	}

	// Email regex
	emailRegex := regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	matches := emailRegex.FindAllString(text, -1)
	
	seenEmails := make(map[string]bool)
	for _, email := range matches {
		email = strings.ToLower(email)
		if !seenEmails[email] {
			emails = append(emails, email)
			seenEmails[email] = true
		}
	}

	// Twitter/X handle extraction
	seenHandles := make(map[string]bool)
	
	// 1. Matches @handle logic
	words := strings.Fields(text)
	for _, word := range words {
		if strings.HasPrefix(word, "@") && len(word) > 1 {
			handle := strings.TrimRight(word, ".,;:!?")
			if matched, _ := regexp.MatchString(`^@[a-zA-Z0-9_]+$`, handle); matched {
				handle = strings.ToLower(handle)
				if !seenHandles[handle] {
					handles = append(handles, handle)
					seenHandles[handle] = true
				}
			}
		}
	}

	// 2. Matches URL patterns
	urlRegex := regexp.MustCompile(`https?://(?:www\.)?(?:twitter\.com|x\.com)/([a-zA-Z0-9_]+)`)
	urlMatches := urlRegex.FindAllStringSubmatch(text, -1)
	for _, match := range urlMatches {
		if len(match) > 1 {
			handle := "@" + strings.ToLower(match[1])
			if !seenHandles[handle] {
				handles = append(handles, handle)
				seenHandles[handle] = true
			}
		}
	}

	return emails, handles
}

// ProcessReportDescription extracts and saves contacts from report description
func (s *ContactService) ProcessReportDescription(brandName string, description string) error {
	log.Printf("ProcessReportDescription called for brand %q with description: %q", brandName, description)
	
	emails, handles := ExtractContactsFromText(description)
	
	if len(emails) == 0 && len(handles) == 0 {
		log.Printf("ProcessReportDescription: No emails or handles extracted from description")
		return nil
	}
	
	log.Printf("Extracted from description for %s: %d emails (%v), %d handles (%v)", brandName, len(emails), emails, len(handles), handles)

	// Check existing contacts to prevent overwrites or duplicates
	existingContacts, err := s.GetContactsForBrand(brandName)
	if err != nil {
		return err
	}

	existingEmailMap := make(map[string]bool)
	existingHandleMap := make(map[string]bool)
	for _, c := range existingContacts {
		if c.Email != "" {
			existingEmailMap[strings.ToLower(c.Email)] = true
		}
		if c.TwitterHandle != "" {
			existingHandleMap[strings.ToLower(c.TwitterHandle)] = true
		}
	}
	
	// Save new emails
	for _, email := range emails {
		if existingEmailMap[email] {
			continue
		}
		contact := &Contact{
			BrandName:    brandName,
			ContactName:  "User Reported Contact",
			ContactTitle: "Reported via App",
			ContactLevel: "ic",
			Email:        email,
			Source:       "manual",
			EmailVerified: true,
		}
		if err := s.SaveContact(contact); err != nil {
			log.Printf("Failed to save user-reported email %s: %v", email, err)
		}
	}
	
	// Save new handles
	for _, handle := range handles {
		if existingHandleMap[handle] {
			continue
        }
		contact := &Contact{
			BrandName:     brandName,
			ContactName:   "User Reported Contact",
			ContactTitle:  "Reported via App",
			ContactLevel:  "ic",
			TwitterHandle: handle,
			Source:        "manual",
		}
		if err := s.SaveContact(contact); err != nil {
			log.Printf("Failed to save user-reported handle %s: %v", handle, err)
		}
	}
	
	return nil
}
