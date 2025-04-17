package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// --- Configuration ---
const (
	issuesJSONPath     = "issues.json"
	milestonesJSONPath = "milestones.json"
	labelsJSONPath     = "labels.json"
	githubAPIBaseURL   = "https://api.github.com"
	requestDelay       = 1 * time.Second // Delay to avoid hitting rate limits
)

// --- Structs for JSON Data ---

// LabelData matches the structure in labels.json
type LabelData struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"` // Color hex code without '#'
}

// MilestoneData matches the structure in milestones.json
type MilestoneData struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	DueOn       *string `json:"due_on,omitempty"` // Use pointer for optionality
}

// IssueData matches the structure in issues.json, uses Milestone Title
type IssueData struct {
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Labels         []string `json:"labels"`                    // Uses label names
	MilestoneTitle *string  `json:"milestone_title,omitempty"` // Link by title
}

// --- Structs for GitHub API Payloads & Responses ---

// GitHubLabelRequest is the payload for creating/updating a label
type GitHubLabelRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Color       string `json:"color"` // Color hex code without '#'
}

// GitHubLabelResponse represents a label returned by the API
type GitHubLabelResponse struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// GitHubMilestoneRequest is the payload for creating/updating a milestone
type GitHubMilestoneRequest struct {
	Title       string  `json:"title"`
	State       string  `json:"state,omitempty"` // e.g., "open"
	Description string  `json:"description,omitempty"`
	DueOn       *string `json:"due_on,omitempty"` // Format: "2012-10-09T23:39:01Z"
}

// GitHubMilestoneResponse represents a milestone returned by the API
type GitHubMilestoneResponse struct {
	ID     int    `json:"number"` // GitHub uses 'number' for milestone ID
	NodeID string `json:"node_id"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	State  string `json:"state"`
}

// GitHubIssueRequest is the payload structure for the GitHub API
type GitHubIssueRequest struct {
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Labels    []string `json:"labels,omitempty"`    // Uses label names
	Milestone *int     `json:"milestone,omitempty"` // API field name is 'milestone' (the number/ID)
}

// --- Global Variables ---
var (
	githubToken string
	owner       string
	repo        string
	httpClient  *http.Client
)

// --- Helper Functions ---

// sendGitHubRequest sends a request to the GitHub API
func sendGitHubRequest(ctx context.Context, method, url string, payload interface{}) (*http.Response, []byte, error) {
	var reqBody io.Reader
	if payload != nil {
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			return nil, nil, fmt.Errorf("error marshalling payload for %s %s: %w", method, url, err)
		}
		reqBody = bytes.NewBuffer(payloadBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating request for %s %s: %w", method, url, err)
	}

	req.Header.Set("Authorization", "Bearer "+githubToken) // Use Bearer token
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28") // Recommended header

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("error sending request for %s %s: %w", method, url, err)
	}
	defer resp.Body.Close()

	bodyBytes, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		log.Printf("Warning: could not read response body for %s %s: %v", method, url, readErr)
	}

	// Handle rate limiting specifically
	if resp.StatusCode == http.StatusForbidden && strings.Contains(string(bodyBytes), "rate limit exceeded") {
		log.Printf("Rate limit exceeded. Consider increasing requestDelay.")
		// Potentially add retry logic here
	}

	return resp, bodyBytes, nil
}

// getExistingLabels fetches all labels from the repo
func getExistingLabels(ctx context.Context) (map[string]bool, error) {
	labelsMap := make(map[string]bool)
	url := fmt.Sprintf("%s/repos/%s/%s/labels?per_page=100", githubAPIBaseURL, owner, repo)
	page := 1

	for {
		pageURL := fmt.Sprintf("%s&page=%d", url, page)
		log.Printf("Fetching existing labels (page %d)...", page)
		resp, bodyBytes, err := sendGitHubRequest(ctx, "GET", pageURL, nil)
		if err != nil {
			return nil, fmt.Errorf("error fetching labels page %d: %w", page, err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("error fetching labels page %d: status %d, body: %s", page, resp.StatusCode, string(bodyBytes))
		}

		var labels []GitHubLabelResponse
		if err := json.Unmarshal(bodyBytes, &labels); err != nil {
			return nil, fmt.Errorf("error unmarshalling labels page %d: %w", page, err)
		}

		if len(labels) == 0 {
			break // No more labels on subsequent pages
		}

		for _, l := range labels {
			labelsMap[l.Name] = true // Store label name as key
		}
		log.Printf("Fetched %d labels on page %d.", len(labels), page)

		// Check Link header for next page (basic check)
		linkHeader := resp.Header.Get("Link")
		if !strings.Contains(linkHeader, `rel="next"`) {
			break // No next page indicated
		}
		page++
		time.Sleep(requestDelay) // Be nice to the API
	}

	log.Printf("Found %d existing labels.", len(labelsMap))
	return labelsMap, nil
}

// createLabel creates a single label
func createLabel(ctx context.Context, label LabelData) error {
	url := fmt.Sprintf("%s/repos/%s/%s/labels", githubAPIBaseURL, owner, repo)
	payload := GitHubLabelRequest{
		Name:        label.Name,
		Description: label.Description,
		Color:       label.Color,
	}

	log.Printf("Attempting to create label: \"%s\"", label.Name)
	resp, bodyBytes, err := sendGitHubRequest(ctx, "POST", url, payload)
	if err != nil {
		return fmt.Errorf("error sending create label request for '%s': %w", label.Name, err)
	}

	// GitHub returns 201 Created on success
	if resp.StatusCode != http.StatusCreated {
		// Check if it already exists (Conflict - 422 Unprocessable Entity)
		if resp.StatusCode == http.StatusUnprocessableEntity && strings.Contains(string(bodyBytes), "already_exists") {
			log.Printf("Label \"%s\" already exists (API reported conflict).", label.Name)
			return nil // Not an error in our case, just skip
		}
		return fmt.Errorf("error creating label '%s': status %d, body: %s", label.Name, resp.StatusCode, string(bodyBytes))
	}

	log.Printf("Successfully created label: \"%s\"\n", label.Name)
	return nil
}

// getExistingMilestones fetches all open and closed milestones from the repo
func getExistingMilestones(ctx context.Context) (map[string]int, error) {
	milestonesMap := make(map[string]int)
	// Fetch both open and closed to avoid creating duplicates if one was closed manually
	url := fmt.Sprintf("%s/repos/%s/%s/milestones?state=all&per_page=100", githubAPIBaseURL, owner, repo)
	page := 1

	for {
		pageURL := fmt.Sprintf("%s&page=%d", url, page)
		log.Printf("Fetching existing milestones (page %d)...", page)
		resp, bodyBytes, err := sendGitHubRequest(ctx, "GET", pageURL, nil)
		if err != nil {
			return nil, fmt.Errorf("error fetching milestones page %d: %w", page, err)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("error fetching milestones page %d: status %d, body: %s", page, resp.StatusCode, string(bodyBytes))
		}

		var milestones []GitHubMilestoneResponse
		if err := json.Unmarshal(bodyBytes, &milestones); err != nil {
			return nil, fmt.Errorf("error unmarshalling milestones page %d: %w", page, err)
		}

		if len(milestones) == 0 {
			break // No more milestones on subsequent pages
		}

		for _, m := range milestones {
			milestonesMap[m.Title] = m.ID
		}
		log.Printf("Fetched %d milestones on page %d.", len(milestones), page)

		// Check Link header for next page (basic check)
		linkHeader := resp.Header.Get("Link")
		if !strings.Contains(linkHeader, `rel="next"`) {
			break // No next page indicated
		}
		page++
		time.Sleep(requestDelay) // Be nice to the API
	}

	log.Printf("Found %d existing milestones.", len(milestonesMap))
	return milestonesMap, nil
}

// createMilestone creates a single milestone
func createMilestone(ctx context.Context, milestone MilestoneData) (int, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/milestones", githubAPIBaseURL, owner, repo)
	payload := GitHubMilestoneRequest{
		Title:       milestone.Title,
		Description: milestone.Description,
		State:       "open", // Default to open
		DueOn:       milestone.DueOn,
	}

	log.Printf("Attempting to create milestone: \"%s\"", milestone.Title)
	resp, bodyBytes, err := sendGitHubRequest(ctx, "POST", url, payload)
	if err != nil {
		return 0, fmt.Errorf("error sending create milestone request for '%s': %w", milestone.Title, err)
	}

	if resp.StatusCode != http.StatusCreated {
		return 0, fmt.Errorf("error creating milestone '%s': status %d, body: %s", milestone.Title, resp.StatusCode, string(bodyBytes))
	}

	var createdMilestone GitHubMilestoneResponse
	if err := json.Unmarshal(bodyBytes, &createdMilestone); err != nil {
		return 0, fmt.Errorf("error unmarshalling created milestone response for '%s': %w", milestone.Title, err)
	}

	log.Printf("Successfully created milestone: \"%s\" (ID: %d)\n", createdMilestone.Title, createdMilestone.ID)
	return createdMilestone.ID, nil
}

// createIssue creates a single issue
func createIssue(ctx context.Context, issue IssueData, milestoneID *int) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues", githubAPIBaseURL, owner, repo)
	payload := GitHubIssueRequest{
		Title:     issue.Title,
		Body:      issue.Description,
		Labels:    issue.Labels, // Pass label names directly
		Milestone: milestoneID,  // Assign the actual ID (pointer)
	}

	log.Printf("Attempting to create issue: \"%s\" (Milestone ID: %v, Labels: %v)", issue.Title, milestoneID, issue.Labels)
	resp, bodyBytes, err := sendGitHubRequest(ctx, "POST", url, payload)
	if err != nil {
		return fmt.Errorf("error sending create issue request for '%s': %w", issue.Title, err)
	}

	if resp.StatusCode != http.StatusCreated {
		// Check for label validation errors (often 422)
		if resp.StatusCode == http.StatusUnprocessableEntity && strings.Contains(string(bodyBytes), "invalid label") {
			log.Printf("Error creating issue '%s': One or more labels might not exist or are invalid. Body: %s", issue.Title, string(bodyBytes))
			return fmt.Errorf("error creating issue '%s': invalid labels. Body: %s", issue.Title, string(bodyBytes))
		}
		return fmt.Errorf("error creating issue '%s': status %d, body: %s", issue.Title, resp.StatusCode, string(bodyBytes))
	}

	log.Printf("Successfully created issue: \"%s\"\n", issue.Title)
	return nil
}

// --- Processing Functions ---

// processLabels ensures labels defined in labels.json exist
func processLabels(ctx context.Context) (int, error) {
	log.Printf("--- Processing Labels from %s ---", labelsJSONPath)
	jsonData, err := os.ReadFile(labelsJSONPath)
	if err != nil {
		return 0, fmt.Errorf("error reading labels file %s: %w", labelsJSONPath, err)
	}
	var labelsToProcess []LabelData
	if err := json.Unmarshal(jsonData, &labelsToProcess); err != nil {
		return 0, fmt.Errorf("error unmarshalling labels JSON: %w", err)
	}
	log.Printf("Read %d label definitions from JSON.", len(labelsToProcess))

	existingLabelsMap, err := getExistingLabels(ctx)
	if err != nil {
		return 0, fmt.Errorf("error getting existing labels: %w", err)
	}

	createdCount := 0
	for _, label := range labelsToProcess {
		if _, exists := existingLabelsMap[label.Name]; !exists {
			err := createLabel(ctx, label)
			if err != nil {
				log.Printf("Failed to create label '%s': %v. Continuing...", label.Name, err)
				// Continue processing other labels even if one fails
			} else {
				createdCount++
				time.Sleep(requestDelay)
			}
		} else {
			log.Printf("Label \"%s\" already exists.", label.Name)
		}
	}
	log.Printf("Finished processing labels. Created %d new labels.", createdCount)
	return createdCount, nil
}

// processMilestones ensures milestones defined in milestones.json exist and returns a map
func processMilestones(ctx context.Context) (map[string]int, int, error) {
	log.Printf("--- Processing Milestones from %s ---", milestonesJSONPath)
	jsonData, err := os.ReadFile(milestonesJSONPath)
	if err != nil {
		return nil, 0, fmt.Errorf("error reading milestones file %s: %w", milestonesJSONPath, err)
	}
	var milestonesToProcess []MilestoneData
	if err := json.Unmarshal(jsonData, &milestonesToProcess); err != nil {
		return nil, 0, fmt.Errorf("error unmarshalling milestones JSON: %w", err)
	}
	log.Printf("Read %d milestones definitions from JSON.", len(milestonesToProcess))

	existingMilestonesMap, err := getExistingMilestones(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("error getting existing milestones: %w", err)
	}

	milestoneTitleToIDMap := make(map[string]int)
	createdCount := 0

	// Populate map with existing milestones first
	for title, id := range existingMilestonesMap {
		milestoneTitleToIDMap[title] = id
	}

	// Create missing milestones
	for _, milestone := range milestonesToProcess {
		if _, exists := milestoneTitleToIDMap[milestone.Title]; !exists {
			newID, err := createMilestone(ctx, milestone)
			if err != nil {
				log.Printf("Failed to create milestone '%s': %v. Continuing...", milestone.Title, err)
				continue // Skip trying to use this milestone later if creation failed
			}
			milestoneTitleToIDMap[milestone.Title] = newID // Add newly created milestone to map
			createdCount++
			time.Sleep(requestDelay)
		} else {
			log.Printf("Milestone \"%s\" already exists.", milestone.Title)
		}
	}
	log.Printf("Finished processing milestones. Created %d new milestones.", createdCount)
	log.Printf("Current Milestone Title -> ID Map: %v", milestoneTitleToIDMap) // Log the map
	return milestoneTitleToIDMap, createdCount, nil
}

// processIssues creates issues defined in issues.json, linking to milestones
func processIssues(ctx context.Context, milestoneTitleToIDMap map[string]int) (int, error) {
	log.Printf("--- Processing Issues from %s ---", issuesJSONPath)
	jsonData, err := os.ReadFile(issuesJSONPath)
	if err != nil {
		return 0, fmt.Errorf("error reading issues file %s: %w", issuesJSONPath, err)
	}
	var issuesToCreate []IssueData
	if err := json.Unmarshal(jsonData, &issuesToCreate); err != nil {
		return 0, fmt.Errorf("error unmarshalling issues JSON: %w", err)
	}
	log.Printf("Read %d issue definitions from JSON.", len(issuesToCreate))

	createdCount := 0
	for _, issue := range issuesToCreate {
		var milestoneID *int // Pointer to int, defaults to nil

		// Find the milestone ID using the title from the map
		if issue.MilestoneTitle != nil && *issue.MilestoneTitle != "" {
			if id, found := milestoneTitleToIDMap[*issue.MilestoneTitle]; found {
				milestoneID = &id // Assign the address of the found ID
			} else {
				log.Printf("Warning: Milestone title '%s' specified for issue '%s' not found or failed to create. Issue will be created without a milestone.", *issue.MilestoneTitle, issue.Title)
			}
		}

		// Create the issue, passing label names directly
		err := createIssue(ctx, issue, milestoneID)
		if err != nil {
			log.Printf("Failed to create issue '%s': %v", issue.Title, err)
			// Decide if you want to stop on failure or continue
			// continue
		} else {
			createdCount++
		}
		time.Sleep(requestDelay) // Delay between issue creations
	}
	log.Printf("Finished processing issues. Created %d new issues.", createdCount)
	return createdCount, nil
}

// --- Main Execution ---

func main() {
	ctx := context.Background()
	httpClient = &http.Client{Timeout: 20 * time.Second} // Increased timeout slightly

	// --- Configuration ---
	githubToken = os.Getenv("GITHUB_TOKEN")
	githubRepo := os.Getenv("GITHUB_REPOSITORY") // Expects "owner/repo" format

	if githubToken == "" {
		log.Fatal("Error: GITHUB_TOKEN environment variable not set.")
	}
	if githubRepo == "" {
		log.Fatal("Error: GITHUB_REPOSITORY environment variable not set.")
	}
	repoParts := strings.Split(githubRepo, "/")
	if len(repoParts) != 2 {
		log.Fatalf("Error: Invalid GITHUB_REPOSITORY format: %s. Expected 'owner/repo'.", githubRepo)
	}
	owner = repoParts[0]
	repo = repoParts[1]

	log.Printf("Target Repository: %s/%s", owner, repo)

	// --- Step 1: Process Labels ---
	labelsCreatedCount, err := processLabels(ctx)
	if err != nil {
		// Decide if label processing failure is fatal
		log.Printf("Warning: Error during label processing: %v", err)
	}

	// --- Step 2: Process Milestones ---
	milestoneTitleToIDMap, milestonesCreatedCount, err := processMilestones(ctx)
	if err != nil {
		// Decide if milestone processing failure is fatal
		log.Fatalf("Error during milestone processing: %v", err) // Making this fatal as issues depend on the map
	}

	// --- Step 3: Process Issues ---
	issuesCreatedCount, err := processIssues(ctx, milestoneTitleToIDMap)
	if err != nil {
		// Log error but report counts anyway
		log.Printf("Warning: Error during issue processing: %v", err)
	}

	log.Printf("--- Final Summary ---")
	log.Printf("Labels processed: %d created.", labelsCreatedCount)
	log.Printf("Milestones processed: %d created.", milestonesCreatedCount)
	log.Printf("Issues processed: %d created.", issuesCreatedCount)
}
