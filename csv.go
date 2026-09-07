package projektovemeeting

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
)

// canonical column labels. Matching is case-insensitive and trims whitespace.
const (
	csvColSubject     = "subject"
	csvColDescription = "description"
	csvColProject     = "project"
	csvColStartDate   = "start date"
	csvColDueDate     = "due date"
	csvColAssignee    = "assignee"
)

var csvRequiredCols = []string{csvColSubject, csvColProject, csvColStartDate, csvColDueDate}

func normalizeCSVHeader(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// parseBatchCSV parses an uploaded batch CSV into issue creation objects.
// It validates structure (required columns and row values), formats and that
// project/assignee ids actually exist in the given Projektove data.
func parseBatchCSV(raw string, projects []ProjektoveProject, users []ProjektoveUser) ([]IssueCreate, error) {
	reader := csv.NewReader(strings.NewReader(raw))
	records, err := reader.ReadAll()
	if err != nil {
		return nil, &BatchCSVError{Messages: []string{fmt.Sprintf("could not parse the csv table: %v", err)}}
	}

	if len(records) < 2 {
		return nil, &BatchCSVError{Messages: []string{"the table must contain a header row and at least one data row"}}
	}

	cols := map[string]int{}
	for i, cell := range records[0] {
		key := normalizeCSVHeader(cell)
		if key != "" {
			cols[key] = i
		}
	}

	var messages []string
	for _, required := range csvRequiredCols {
		if _, ok := cols[required]; !ok {
			messages = append(messages, fmt.Sprintf("missing required column %q", required))
		}
	}
	if len(messages) > 0 {
		return nil, &BatchCSVError{Messages: messages}
	}

	projectIDs := make(map[int]bool, len(projects))
	for _, p := range projects {
		projectIDs[p.ID] = true
	}
	userIDs := make(map[int]bool, len(users))
	for _, u := range users {
		userIDs[u.ID] = true
	}

	objs := []IssueCreate{}
	for i, record := range records[1:] {
		row := i + 2 // 1-based, header is row 1
		obj := IssueCreate{
			Subject: cellAt(record, cols[csvColSubject]),
		}
		if c, ok := cols[csvColDescription]; ok {
			obj.Description = cellAt(record, c)
		}
		rowMsgs := []string{}

		if obj.Subject == "" {
			rowMsgs = append(rowMsgs, "subject is required")
		}

		projectRaw := cellAt(record, cols[csvColProject])
		if projectRaw == "" {
			rowMsgs = append(rowMsgs, "project is required")
		} else if projectID, err := strconv.Atoi(projectRaw); err != nil {
			rowMsgs = append(rowMsgs, fmt.Sprintf("project %q is not a valid number", projectRaw))
		} else if !projectIDs[projectID] {
			rowMsgs = append(rowMsgs, fmt.Sprintf("project id %d does not exist in Projektove", projectID))
		} else {
			obj.ProjectID = projectID
		}

		startRaw := cellAt(record, cols[csvColStartDate])
		if startRaw == "" {
			rowMsgs = append(rowMsgs, "start date is required")
		} else if startDate, err := parseDate(startRaw); err != nil {
			rowMsgs = append(rowMsgs, fmt.Sprintf("start date %q must use YYYY-MM-DD format", startRaw))
		} else {
			obj.StartDate = startDate
		}

		dueRaw := cellAt(record, cols[csvColDueDate])
		if dueRaw == "" {
			rowMsgs = append(rowMsgs, "due date is required")
		} else if dueDate, err := parseDate(dueRaw); err != nil {
			rowMsgs = append(rowMsgs, fmt.Sprintf("due date %q must use YYYY-MM-DD format", dueRaw))
		} else {
			obj.DueDate = dueDate
		}

		assigneeRaw := ""
		if c, ok := cols[csvColAssignee]; ok {
			assigneeRaw = cellAt(record, c)
		}
		if assigneeRaw != "" {
			assigneeID, err := strconv.Atoi(assigneeRaw)
			if err != nil {
				rowMsgs = append(rowMsgs, fmt.Sprintf("assignee %q is not a valid number", assigneeRaw))
			} else if !userIDs[assigneeID] {
				rowMsgs = append(rowMsgs, fmt.Sprintf("assignee id %d does not exist in Projektove", assigneeID))
			} else {
				obj.AssignedToID = assigneeID
			}
		}

		if len(rowMsgs) > 0 {
			messages = append(messages, fmt.Sprintf("row %d: %s", row, strings.Join(rowMsgs, ", ")))
			continue
		}

		objs = append(objs, obj)
	}

	if len(messages) > 0 {
		return nil, &BatchCSVError{Messages: messages}
	}

	return objs, nil
}

func cellAt(record []string, col int) string {
	if col < 0 || col >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[col])
}
