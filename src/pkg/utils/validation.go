package utils

import (
	"fmt"
	"regexp"
)

// OwnerRepoPattern validates owner/repo format
var OwnerRepoPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,38}[a-zA-Z0-9])?/[a-zA-Z0-9._-]{1,100}$`)

// ValidDatePattern validates date filter format
var ValidDatePattern = regexp.MustCompile(`^([><=]?\d{4}-\d{2}-\d{2}|\d{4}-\d{2}-\d{2}\.\.\d{4}-\d{2}-\d{2})$`)

// ValidateState validates the state parameter for issues/PRs API
func ValidateState(state string) error {
	if state == "" {
		return nil
	}
	validStates := map[string]bool{"open": true, "closed": true, "all": true}
	if !validStates[state] {
		return fmt.Errorf("invalid state: %s. Must be one of: open, closed, all", state)
	}
	return nil
}

// ValidateListLimit validates the limit parameter (1-100)
func ValidateListLimit(limit int) error {
	if limit < 1 || limit > MaxListLimit {
		return fmt.Errorf("limit must be between 1 and %d", MaxListLimit)
	}
	return nil
}

// ValidateBranchFilter validates branch filter parameters
func ValidateBranchFilter(filter, name string) error {
	if len(filter) > MaxBranchFilterLen {
		return fmt.Errorf("%s filter exceeds maximum length of %d characters", name, MaxBranchFilterLen)
	}
	return nil
}

// ValidateQuery validates search query length constraints.
func ValidateQuery(query string) error {
	if query == "" {
		return fmt.Errorf("missing required parameter: query")
	}
	if len(query) > MaxSearchQueryLen {
		return fmt.Errorf("query exceeds maximum length of %d characters", MaxSearchQueryLen)
	}
	return nil
}

// ValidateSearchFilters validates the length and format of search filter parameters
func ValidateSearchFilters(language, filename, author, date string) error {
	if len(language) > MaxLanguageFilterLen {
		return fmt.Errorf("language filter exceeds maximum length of %d characters", MaxLanguageFilterLen)
	}

	if len(filename) > MaxFilenameFilterLen {
		return fmt.Errorf("filename filter exceeds maximum length of %d characters", MaxFilenameFilterLen)
	}

	if len(author) > MaxAuthorFilterLen {
		return fmt.Errorf("author filter exceeds maximum length of %d characters", MaxAuthorFilterLen)
	}

	if date != "" {
		if len(date) > MaxDateFilterLen {
			return fmt.Errorf("date filter exceeds maximum length of %d characters", MaxDateFilterLen)
		}
		// Validate date format: >YYYY-MM-DD, <YYYY-MM-DD, YYYY-MM-DD..YYYY-MM-DD
		if !ValidDatePattern.MatchString(date) {
			return fmt.Errorf("invalid date format. Use: >YYYY-MM-DD, <YYYY-MM-DD, or YYYY-MM-DD..YYYY-MM-DD")
		}
	}

	return nil
}
