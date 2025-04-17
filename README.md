# GitHub Project Setup Automation

This directory contains configuration files and a Go script to automatically create labels, milestones, and issues in a GitHub repository using GitHub Actions.

## Purpose

This setup allows you to quickly bootstrap a new project's structure in GitHub Issues, ensuring consistency across projects.

## Files

*   `labels.json`: Defines the standard labels to be created in the repository. Edit this file to add, remove, or modify labels specific to your project.
*   `milestones.json`: Defines the project milestones (phases, sprints, releases). Edit this file to reflect your project's timeline. The `title` field is used to link issues.
*   `issues.json`: Defines the initial set of issues to be created. Use the `labels` array (with exact names from `labels.json`) and `milestone_title` (with exact titles from `milestones.json`) to link them.
*   `main.go`: The Go script that interacts with the GitHub API to fetch existing items and create missing ones based on the JSON definitions. **(Usually no changes needed)**.

## Workflow

*   `.github/workflows/create-project-setup.yml`: The GitHub Actions workflow that checks out the code, sets up Go, and runs the `main.go` script. **(Usually no changes needed)**.

## How to Use for a New Project

1.  **Copy Files:** Copy this `project_setup` directory and the `.github/workflows/create-project-setup.yml` file into the root of your new project repository.
2.  **Customize `labels.json`:** Define the set of labels you want for this specific project.
3.  **Customize `milestones.json`:** Define the milestones relevant to this project's lifecycle.
4.  **Customize `issues.json`:** Define the initial backlog of issues, ensuring the `labels` and `milestone_title` fields correctly reference the names/titles defined in the other JSON files.
5.  **Commit & Push:** Commit these files to your repository.
6.  **Run Workflow:** Navigate to the "Actions" tab in your GitHub repository, select the "Create/Update Project Setup" workflow, and manually trigger it using the "Run workflow" button.
7.  **Verify:** Check your repository's "Issues" and "Milestones" sections to confirm the items were created as expected. Review the workflow run logs for details or errors.

## Prerequisites

*   The GitHub Action requires `issues: write` and `contents: read` permissions (provided in the workflow file).
*   If running `main.go` locally, you need Go installed and must set the `GITHUB_TOKEN` and `GITHUB_REPOSITORY` environment variables.

## NB
**Important Limitation: JSON Comments**

The standard JSON format does **not** support comments (`// ...` or `/* ... */`). While comments are included in the template `.json` files (`labels.json`, `milestones.json`, `issues.json`) for better readability and guidance, they **must be removed** before running the GitHub Actions workflow. The Go script uses the standard Go JSON parser, which will fail if comments are present. Ensure your final JSON files are strictly valid JSON before triggering the automation.
