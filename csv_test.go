package projektovemeeting

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validCSV() string {
	return "Subject, Description, Project, Start date, Due date, Assignee\n" +
		"task one, some details, 1, 2026-01-01, 2026-02-01, 1\n" +
		"task two, , 2, 2026-03-01, 2026-04-01, \n"
}

func TestParseBatchCSV_Valid(t *testing.T) {
	projects := []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}, {ID: 2, Name: "p2", Description: "d2"}}
	users := []ProjektoveUser{{ID: 1, Name: "u1"}}

	objs, err := parseBatchCSV(validCSV(), projects, users)
	require.NoError(t, err)
	require.Len(t, objs, 2)

	assert.Equal(t, "task one", objs[0].Subject)
	assert.Equal(t, "some details", objs[0].Description)
	assert.Equal(t, 1, objs[0].ProjectID)
	assert.Equal(t, 2026, objs[0].StartDate.Year())
	assert.Equal(t, time.February, objs[0].DueDate.Month())
	assert.Equal(t, 1, objs[0].AssignedToID)

	assert.Equal(t, "task two", objs[1].Subject)
	assert.Empty(t, objs[1].Description)
	assert.Equal(t, 2, objs[1].ProjectID)
	assert.Zero(t, objs[1].AssignedToID)
}

func TestParseBatchCSV_HeaderCaseInsensitiveAndWhitespace(t *testing.T) {
	projects := []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}}
	users := []ProjektoveUser{{ID: 1, Name: "u1"}}

	raw := " sUbJeCt , project , START DATE , due date \n" +
		"x, 1, 2026-01-01, 2026-02-01\n"

	objs, err := parseBatchCSV(raw, projects, users)
	require.NoError(t, err)
	require.Len(t, objs, 1)
	assert.Equal(t, "x", objs[0].Subject)
}

func TestParseBatchCSV_MissingRequiredColumn(t *testing.T) {
	raw := "Subject, Project, Due date\nx, 1, 2026-02-01\n"

	_, err := parseBatchCSV(raw, nil, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBatchCSVInvalid))
	var csvErr *BatchCSVError
	require.ErrorAs(t, err, &csvErr)
	assert.Contains(t, csvErr.Messages, `missing required column "start date"`)
}

func TestParseBatchCSV_EmptyTable(t *testing.T) {
	_, err := parseBatchCSV("Subject, Project, Start date, Due date\n", nil, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBatchCSVInvalid))
}

func TestParseBatchCSV_RowErrors(t *testing.T) {
	projects := []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}}
	users := []ProjektoveUser{{ID: 1, Name: "u1"}}

	raw := "Subject, Project, Start date, Due date, Assignee\n" +
		", abc, nope, 2026-02-01, 1\n" +
		"ok, 99, 2026-01-01, 2026-02-01, 99\n" +
		"fine, 1, 2026-01-01, bad, 1\n"

	_, err := parseBatchCSV(raw, projects, users)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBatchCSVInvalid))
	var csvErr *BatchCSVError
	require.ErrorAs(t, err, &csvErr)
	require.Len(t, csvErr.Messages, 3)
	assert.Contains(t, csvErr.Messages[0], "row 2")
	assert.Contains(t, csvErr.Messages[0], "subject is required")
	assert.Contains(t, csvErr.Messages[0], `not a valid number`)
	assert.Contains(t, csvErr.Messages[0], "must use YYYY-MM-DD")
	assert.Contains(t, csvErr.Messages[1], "row 3")
	assert.Contains(t, csvErr.Messages[1], "project id 99 does not exist")
	assert.Contains(t, csvErr.Messages[1], "assignee id 99 does not exist")
	assert.Contains(t, csvErr.Messages[2], "row 4")
	assert.Contains(t, csvErr.Messages[2], "must use YYYY-MM-DD")
}

func TestParseBatchCSV_ExtraColumnsIgnored(t *testing.T) {
	projects := []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}}

	raw := "Subject, Project, Start date, Due date, Assigned to, Notes\n" +
		"x, 1, 2026-01-01, 2026-02-01, juniors, whatever\n"

	objs, err := parseBatchCSV(raw, projects, nil)
	require.NoError(t, err)
	require.Len(t, objs, 1)
	assert.Equal(t, "x", objs[0].Subject)
	// "assigned to" is not a recognized column so it is ignored, not an error
	assert.Zero(t, objs[0].AssignedToID)
}

func TestParseBatchCSV_BlankLinesSkipped(t *testing.T) {
	projects := []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}}

	raw := "Subject, Project, Start date, Due date\n\nx, 1, 2026-01-01, 2026-02-01\n\n"
	objs, err := parseBatchCSV(raw, projects, nil)
	require.NoError(t, err)
	require.Len(t, objs, 1)
}

func TestParseBatchCSV_Malformed(t *testing.T) {
	_, err := parseBatchCSV("Subject, Project, \"unclosed", nil, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrBatchCSVInvalid))
}

func TestParseBatchCSV_MixedGoodAndBadRows(t *testing.T) {
	projects := []ProjektoveProject{{ID: 1, Name: "p1", Description: "d1"}}

	raw := "Subject, Project, Start date, Due date\n" +
		"good, 1, 2026-01-01, 2026-02-01\n" +
		"bad, 2, 2026-01-01, 2026-02-01\n" +
		"also bad, , 2026-01-01, 2026-02-01\n"

	// any invalid row fails the whole table
	_, err := parseBatchCSV(raw, projects, nil)
	require.Error(t, err)
	var csvErr *BatchCSVError
	require.ErrorAs(t, err, &csvErr)
	require.Len(t, csvErr.Messages, 2)
}
