package be

import (
	"database/sql"
	"fmt"
	"reflect"
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

func TestReadReport(t *testing.T) {
	it(func() {
		testCases := []struct {
			name      string
			seq       int
			seqExists bool
			sharing   string

			expectResponse *ReadReportResponse
			expectError    bool
		}{
			{
				name:      "Request existing report with enabled avatar",
				seq:       123,
				seqExists: true,
				sharing:   "sharing_data_live",
				expectResponse: &ReadReportResponse{
					Id:     "0x1234",
					Image:  []byte{97, 98, 99, 100, 101, 102, 103, 104},
					Avatar: "testuser",
				},
				expectError: false,
			},
			{
				name:      "Request existing report with disabled avatar",
				seq:       123,
				seqExists: true,
				sharing:   "not_sharing_data_live",
				expectResponse: &ReadReportResponse{
					Id:     "0x1234",
					Image:  []byte{97, 98, 99, 100, 101, 102, 103, 104},
					Avatar: "",
				},
				expectError: false,
			},
			{
				name:        "Request non-existing report",
				seq:         99999,
				seqExists:   false,
				sharing:     "sharing_data_live",
				expectResponse: nil,
				expectError: true,
			},
		}

		columns := []string{
			"id",
			"image",
			"avatar",
			"privacy",
		}
		for _, testCase := range testCases {
			values := ""
			if testCase.seqExists {
				values = fmt.Sprintf("0x1234,abcdefgh,testuser,%s", testCase.sharing)
			}
			mock.ExpectQuery("SELECT r.id, r.image, u.avatar, u.privacy FROM reports AS r JOIN users AS u	ON r.id = u.id WHERE r.seq = ?").WithArgs(testCase.seq).
				WillReturnRows(sqlmock.NewRows(columns).
					FromCSVString(values))

			response, err := readReport(db, &ReadReportArgs{
				Seq: testCase.seq,
			})

			if testCase.expectError != (err != nil) {
				t.Errorf("%s, readReport: expected error: %v, got error: %v", testCase.name, testCase.expectError, err)
			}

			if !reflect.DeepEqual(response, testCase.expectResponse) {
				t.Errorf("%s, readReport: expected %v, got %v", testCase.name, testCase.expectResponse, response)
			}
		}
	})
}
