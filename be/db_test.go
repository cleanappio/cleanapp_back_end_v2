package be

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
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

func testRefGen() string {
	return "testrefid"
}

var it = beforeeach.Create(setUp, tearDown)

func TestUpdateOrCreateUser(t *testing.T) {
	it(func() {
		testCases := []struct {
			name     string
			version  string
			id       string
			avatar   string
			referral string
			team     int

			execExpected bool
			rowsAffected int64

			errorExpected bool
		}{
			{
				name:     "Insert or update user",
				version:  "2.0",
				id:       "0x12345678",
				avatar:   "user1",
				referral: "abcdef",
				team:     1,

				execExpected: true,
				rowsAffected: 1,

				errorExpected: false,
			}, {
				name:     "Invalid version",
				version:  "1.0",
				id:       "0x123456768",
				avatar:   "user1",
				referral: "abcdef",
				team:     1,

				execExpected: false,

				errorExpected: true,
			},
		}

		for _, testCase := range testCases {
			if testCase.execExpected {
				mock.ExpectExec(
					"INSERT INTO users \\(id, avatar, referral, team\\) VALUES \\((.+), (.+), (.+), (.+)\\) ON DUPLICATE KEY UPDATE avatar=(.+), referral=(.+), team=(.+)").
					WithArgs(testCase.id, testCase.avatar, testCase.referral, testCase.team, testCase.avatar, testCase.referral, testCase.team).
					WillReturnResult(sqlmock.NewResult(1, testCase.rowsAffected))
			}
			if err := updateUser(db, &UserArgs{
				Version:  testCase.version,
				Id:       testCase.id,
				Avatar:   testCase.avatar,
				Referral: testCase.referral,
			}); testCase.errorExpected != (err != nil) {
				t.Errorf("%s, updateUser: expected error: %v, got error: %v", testCase.name, testCase.errorExpected, err)
			}
		}
	})
}

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
				name:           "Request non-existing report",
				seq:            99999,
				seqExists:      false,
				sharing:        "sharing_data_live",
				expectResponse: nil,
				expectError:    true,
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

func TestReadReferral(t *testing.T) {
	it(func() {
		testCases := []struct {
			name      string
			refKey    string
			refValues []string

			expectedValue string
			errorExpected bool
		}{
			{
				name:      "Found referral",
				refKey:    "192.168.0.34:300:670",
				refValues: []string{"abcdef"},

				expectedValue: "abcdef",
				errorExpected: false,
			},
			{
				name:      "Referral not found",
				refKey:    "192.168.0.34:300:670",
				refValues: []string{},

				expectedValue: "",
				errorExpected: false,
			},
			{
				name:      "Fetch Error",
				refKey:    "192.168.0.34:300:670",
				refValues: []string{},

				errorExpected: true,
			},
		}

		columns := []string{
			"refvalue",
		}
		for _, testCase := range testCases {
			if testCase.errorExpected {
				mock.ExpectQuery("SELECT refvalue	FROM referrals WHERE refkey = (.+)").WithArgs(testCase.refKey).
					WillReturnError(fmt.Errorf("test fetch error"))
			} else {
				mock.ExpectQuery("SELECT refvalue	FROM referrals WHERE refkey = (.+)").WithArgs(testCase.refKey).
					WillReturnRows(sqlmock.NewRows(columns).
						FromCSVString(strings.Join(testCase.refValues, "\n")))
			}

			refvalue, err := readReferral(db, testCase.refKey)
			if testCase.errorExpected != (err != nil) {
				t.Errorf("%s, refDB.ReadReferral: expected error: %v, got error: %e", testCase.name, testCase.errorExpected, err)
			}
			if refvalue != testCase.expectedValue {
				t.Errorf("%s, refDB.ReadReferral: expected %s, got %s", testCase.name, testCase.expectedValue, refvalue)
			}
		}
	})
}

func TestWriteReferral(t *testing.T) {
	it(func() {
		testCases := []struct {
			name      string
			refKey    string
			refValue  string
			refExists bool

			errorExpected bool
		}{
			{
				name:      "New referral",
				refKey:    "192.168.0.34:300:670",
				refValue:  "abcdef",
				refExists: false,

				errorExpected: false,
			},
			{
				name:      "Existing referral",
				refKey:    "192.168.0.34:300:670",
				refValue:  "abcdef",
				refExists: true,

				errorExpected: false,
			},
			{
				name:      "Exec Error",
				refKey:    "192.168.0.34:300:670",
				refValue:  "abcdef",
				refExists: false,

				errorExpected: true,
			},
		}

		columns := []string{
			"refvalue",
		}
		for _, testCase := range testCases {
			if testCase.refExists {
				mock.ExpectQuery("SELECT refvalue	FROM referrals WHERE refkey = (.+)").WithArgs(testCase.refKey).
					WillReturnRows(sqlmock.NewRows(columns).
						FromCSVString(testCase.refValue))
			} else {
				mock.ExpectQuery("SELECT refvalue	FROM referrals WHERE refkey = (.+)").WithArgs(testCase.refKey).
					WillReturnRows(sqlmock.NewRows(columns).
						FromCSVString(""))
			}

			if !testCase.refExists {
				if testCase.errorExpected {
					mock.ExpectExec("INSERT INTO referrals \\(refkey, refvalue\\) VALUES (.+)").
						WithArgs(testCase.refKey, testCase.refValue).
						WillReturnError(fmt.Errorf("update test error"))
				} else {
					mock.ExpectExec("INSERT INTO referrals \\(refkey, refvalue\\) VALUES (.+)").
						WithArgs(testCase.refKey, testCase.refValue).
						WillReturnResult(sqlmock.NewResult(1, 1))
				}
			}

			if err := writeReferral(db, testCase.refKey, testCase.refValue); testCase.errorExpected != (err != nil) {
				t.Errorf("%s, refDB.WriteReferral: expected error: %v, got error: %e", testCase.name, testCase.errorExpected, err)
			}
		}
	})
}

func TestGenerateReferral(t *testing.T) {
	it(func() {
		testCases := []struct {
			name    string
			version string
			id      string
			refcode string

			refExists     bool
			errorExpected bool

			expectedResponse *GenRefResponse
		}{
			{
				name:    "Success referral generation",
				version: "2.0",
				id:      "0x1234",
				refcode: "testrefid",

				refExists:     false,
				errorExpected: false,

				expectedResponse: &GenRefResponse{
					RefValue: "testrefid",
				},
			}, {
				name:    "Success existing referral retrieval",
				version: "2.0",
				id:      "0x5678",
				refcode: "testrefid",

				refExists:     true,
				errorExpected: false,

				expectedResponse: &GenRefResponse{
					RefValue: "testrefid",
				},
			}, {
				name:    "Error in referral generation storing",
				version: "2.0",
				id:      "0x9012",
				refcode: "testrefid",

				refExists:     false,
				errorExpected: true,

				expectedResponse: nil,
			},
		}

		columns := []string{
			"referral",
		}
		for _, testCase := range testCases {
			if testCase.refExists {
				mock.ExpectQuery("SELECT referral FROM users_refcodes WHERE id = (.+)").
					WithArgs(testCase.id).
					WillReturnRows(sqlmock.NewRows(columns).FromCSVString(testCase.refcode))
			} else {
				mock.ExpectQuery("SELECT referral FROM users_refcodes WHERE id = (.+)").
					WithArgs(testCase.id).
					WillReturnRows(sqlmock.NewRows(columns))
				if testCase.errorExpected {
					mock.ExpectExec("INSERT INTO users_refcodes \\(id, referral\\) VALUES \\((.+), (.+)\\)").
						WithArgs(testCase.id, testCase.refcode).
						WillReturnError(fmt.Errorf("ref update error"))
				} else {
					mock.ExpectExec("INSERT INTO users_refcodes \\(id, referral\\) VALUES \\((.+), (.+)\\)").
						WithArgs(testCase.id, testCase.refcode).
						WillReturnResult(sqlmock.NewResult(1, 1))
				}
			}

			response, err := generateReferral(db, &GenRefRequest{
				Version: testCase.version,
				Id:      testCase.id,
			}, testRefGen)

			if testCase.errorExpected != (err != nil) {
				t.Errorf("%s, generateReferral: expected error: %v, got error: %e", testCase.name, testCase.errorExpected, err)
			}

			if !reflect.DeepEqual(response, testCase.expectedResponse) {
				t.Errorf("%s, generateReferral: expected %v, got %v", testCase.name, testCase.expectedResponse, response)
			}
		}
	})
}
