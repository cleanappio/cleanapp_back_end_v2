package be

import (
	"database/sql"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/jknair0/beforeeach"
)

var (
	db   *sql.DB
	mock sqlmock.Sqlmock
)

func setUp() {
	db, mock, _ = sqlmock.New()
}

func tearDown() {
	db.Close()
}

var it = beforeeach.Create(setUp, tearDown)

func TestUpdatePrivacyAndAgreeTOC(t *testing.T) {
	it(func() {
		testCases := []struct {
			name     string
			version  string
			id       string
			privacy  string
			agreeTOC string

			execExpected bool
			rowsAffected int64

			errorExpected bool
		}{
			{
				name:     "Privacy and agreeTOC",
				version:  "2.0",
				id:       "0x123456768",
				privacy:  "privacyVal",
				agreeTOC: "agreeTOCVal",

				execExpected: true,
				rowsAffected: 1,

				errorExpected: false,
			},
			{
				name:    "Privacy only",
				version: "2.0",
				id:      "0x123456768",
				privacy: "privacyVal",

				execExpected: true,
				rowsAffected: 1,

				errorExpected: false,
			},
			{
				name:     "Agree TOC only",
				version:  "2.0",
				id:       "0x123456768",
				agreeTOC: "agreeTOCVal",

				execExpected: true,
				rowsAffected: 1,

				errorExpected: false,
			},
			{
				name:    "No values to update",
				version: "2.0",
				id:      "0x123456768",

				execExpected: false,

				errorExpected: true,
			},
			{
				name:     "Invalid version",
				version:  "1.0",
				id:       "0x123456768",
				privacy:  "privacyVal",
				agreeTOC: "agreeTOCVal",

				execExpected: false,

				errorExpected: true,
			},
		}

		for _, testCase := range testCases {
			if testCase.execExpected {
				if testCase.privacy != "" && testCase.agreeTOC != "" {
					mock.ExpectExec("UPDATE users SET privacy = (.+), agree_toc = (.+) WHERE id = (.+)").
						WithArgs(testCase.privacy, testCase.agreeTOC, testCase.id).
						WillReturnResult(sqlmock.NewResult(1, testCase.rowsAffected))
				} else if testCase.privacy != "" {
					mock.ExpectExec("UPDATE users SET privacy = (.+) WHERE id = (.+)").
						WithArgs(testCase.privacy, testCase.id).
						WillReturnResult(sqlmock.NewResult(1, testCase.rowsAffected))
				} else if testCase.agreeTOC != "" {
					mock.ExpectExec("UPDATE users SET agree_toc = (.+) WHERE id = (.+)").
						WithArgs(testCase.agreeTOC, testCase.id).
						WillReturnResult(sqlmock.NewResult(1, testCase.rowsAffected))
				}
			}
			if err := updatePrivacyAndTOC(db, &PrivacyAndTOCArgs{
				Version:  testCase.version,
				Id:       testCase.id,
				Privacy:  testCase.privacy,
				AgreeTOC: testCase.agreeTOC,
			}); testCase.errorExpected != (err != nil) {
				t.Errorf("%s, updatePrivacyAndTOC: expected error: %v, got error: %v", testCase.name, testCase.errorExpected, err)
			}
		}
	})
}
