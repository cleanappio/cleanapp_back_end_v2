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

func testTeamGen(string) TeamColor {
	return Blue
}

func TestUpdateOrCreateUser(t *testing.T) {
	it(func() {
		testCases := []struct {
			name     string
			version  string
			id       string
			avatar   string
			referral string
			team     int

			retList      []string
			execExpected bool

			expectResponse *UserResp
			expectError    bool
		}{
			{
				name:     "New user",
				version:  "2.0",
				id:       "0x12345678",
				avatar:   "user1",
				referral: "abcdef",
				team:     1,

				retList:      []string{},
				execExpected: true,

				expectResponse: &UserResp{
					Team:      1,
					DupAvatar: false,
				},
				expectError: false,
			}, {
				name:     "Existing user",
				version:  "2.0",
				id:       "0x123456768",
				avatar:   "user1",
				referral: "abcdef",
				team:     1,

				retList:      []string{"0x123456768"},
				execExpected: true,

				expectError: false,
				expectResponse: &UserResp{
					Team:      1,
					DupAvatar: false,
				},
			}, {
				name:     "Duplicate avatar",
				version:  "2.0",
				id:       "0x123456768",
				avatar:   "user1",
				referral: "abcdef",
				team:     1,

				retList:      []string{"0x87654321"},
				execExpected: false,

				expectError: true,
				expectResponse: &UserResp{
					Team:      0,
					DupAvatar: true,
				},
			},
		}

		recordColumns := []string{"id"}
		for _, testCase := range testCases {
			mock.ExpectQuery("SELECT id FROM users WHERE avatar = (.+)").
				WithArgs(testCase.avatar).
				WillReturnRows(
					sqlmock.NewRows(recordColumns).
						FromCSVString(strings.Join(testCase.retList, "\n")))
			if testCase.execExpected {
				mock.ExpectExec(
					"INSERT INTO users \\(id, avatar, referral, team\\) VALUES \\((.+), (.+), (.+), (.+)\\) ON DUPLICATE KEY UPDATE avatar=(.+), referral=(.+), team=(.+)").
					WithArgs(testCase.id, testCase.avatar, testCase.referral, testCase.team, testCase.avatar, testCase.referral, testCase.team).
					WillReturnResult(sqlmock.NewResult(1, 1))
			}
			resp, err := updateUser(db, &UserArgs{
				Version:  testCase.version,
				Id:       testCase.id,
				Avatar:   testCase.avatar,
				Referral: testCase.referral,
			}, testTeamGen)
			if testCase.expectError != (err != nil) {
				t.Errorf("%s, updateUser: expected error: %v, got error: %v", testCase.name, testCase.expectError, err)
			}
			if !reflect.DeepEqual(resp, testCase.expectResponse) {
				t.Errorf("%s, updateUser: expected %v, got %v", testCase.name, testCase.expectResponse, resp)
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

func TestGetTopScores(t *testing.T) {
	it(func() {
		testCases := []struct {
			name         string
			base         *BaseArgs
			topN         int
			retList      []string
			youRet       string
			yourCnt      int
			cntBeforeYou string

			expectResponse *TopScoresResponse
			expectError    bool
		}{
			{
				name: "You're in top",
				base: &BaseArgs{
					Version: "2.0",
					Id:      "0x1234",
				},
				topN: 3,
				retList: []string{
					"0x5678,Ava1,1095",
					"0x1234,AvaYou,1003",
					"0x9012,Ava3, 988",
				},

				expectResponse: &TopScoresResponse{
					Records: []TopScoresRecord{
						{
							Place: 1,
							Title: "Ava1",
							Kitn:  1095,
						}, {
							Place: 2,
							Title: "AvaYou",
							Kitn:  1003,
							IsYou: true,
						}, {
							Place: 3,
							Title: "Ava3",
							Kitn:  988,
						},
					},
				},
			}, {
				name: "You're not in top",
				base: &BaseArgs{
					Version: "2.0",
					Id:      "0x1234",
				},
				topN: 3,
				retList: []string{
					"0x5678,Ava1,1095",
					"0x7777,Ava2,1003",
					"0x9012,Ava3, 988",
				},
				youRet:       "0x1234,AvaYou,99",
				yourCnt:      99,
				cntBeforeYou: "49",

				expectResponse: &TopScoresResponse{
					Records: []TopScoresRecord{
						{
							Place: 1,
							Title: "Ava1",
							Kitn:  1095,
						}, {
							Place: 2,
							Title: "Ava2",
							Kitn:  1003,
						}, {
							Place: 3,
							Title: "Ava3",
							Kitn:  988,
						}, {
							Place: 50,
							Title: "AvaYou",
							Kitn:  99,
							IsYou: true,
						},
					},
				},
			}, {
				name: "Error in query",
				base: &BaseArgs{
					Version: "2.0",
					Id:      "0x1234",
				},
				topN: 3,
				retList: []string{
					"0x5678,Ava1,1095",
					"0x7777,Ava2,1003",
					"0x9012,Ava3, 988",
				},
				youRet:       "0x1234,AvaYou,99",
				yourCnt:      99,
				cntBeforeYou: "49",

				expectResponse: nil,
				expectError:    true,
			},
		}

		recordColumns := []string{"id", "avatar", "cnt"}
		countColunms := []string{"c"}
		for _, testCase := range testCases {
			if testCase.expectError {
				mock.ExpectQuery("SELECT u\\.id, u\\.avatar, count\\(\\*\\) AS cnt FROM reports r JOIN users u ON r\\.id = u\\.id	GROUP BY u\\.id	ORDER BY cnt DESC	LIMIT (.+)").
					WithArgs(testCase.topN).
					WillReturnError(fmt.Errorf("query error"))
			} else {
				mock.ExpectQuery("SELECT u\\.id, u\\.avatar, count\\(\\*\\) AS cnt FROM reports r JOIN users u ON r\\.id = u\\.id	GROUP BY u\\.id	ORDER BY cnt DESC	LIMIT (.+)").
					WithArgs(testCase.topN).
					WillReturnRows(
						sqlmock.NewRows(recordColumns).
							FromCSVString(strings.Join(testCase.retList, "\n")))
			}
			if testCase.youRet != "" {
				mock.ExpectQuery("SELECT u\\.id, u\\.avatar, count\\(\\*\\) AS cnt FROM reports r RIGHT OUTER JOIN users u ON r\\.id = u\\.id WHERE u\\.id = (.+) GROUP BY u\\.id").
					WithArgs(testCase.base.Id).
					WillReturnRows(
						sqlmock.NewRows(recordColumns).
							FromCSVString(testCase.youRet))
				mock.ExpectQuery("SELECT count\\(\\*\\) AS c FROM\\( SELECT id, count\\(\\*\\) AS cnt FROM reports r GROUP BY id HAVING cnt \\> (.+) \\) AS t").
					WithArgs(testCase.yourCnt).
					WillReturnRows(
						sqlmock.NewRows(countColunms).
							FromCSVString(testCase.cntBeforeYou))
			}

			response, err := getTopScores(db, testCase.base, testCase.topN)
			if testCase.expectError != (err != nil) {
				t.Errorf("%s, getTopScores: expected error: %v, got error: %v", testCase.name, testCase.expectError, err)
			}

			if !reflect.DeepEqual(response, testCase.expectResponse) {
				t.Errorf("%s, getTopScores: expected %v, got %v", testCase.name, testCase.expectResponse, response)
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

func TestCleanupReferral(t *testing.T) {
	it(func() {
		testCases := []struct {
			name string
			refkey string

			errorExpected bool
		}{
			{
				name: "Success cleanup",
				refkey: "192.168.1.1:300:700",
				errorExpected: false,
			},{
				name: "Failed cleanup",
				refkey: "192.168.1.2:350:750",
				errorExpected: true,
			},
		}
		for _, testCase := range testCases {
			if testCase.errorExpected {
				mock.ExpectExec("DELETE FROM referrals WHERE refkey = (.+)").
					WithArgs(testCase.refkey).
					WillReturnError(fmt.Errorf("ref delete error"))
			} else {
				mock.ExpectExec("DELETE FROM referrals WHERE refkey = (.+)").
					WithArgs(testCase.refkey).
					WillReturnResult(sqlmock.NewResult(1, 1))
			}

			if err := cleanupReferral(db, testCase.refkey); testCase.errorExpected != (err != nil) {
				t.Errorf("%s, cleanupReferral: expected error: %v, got error: %v", testCase.name, testCase.errorExpected, err)
			}
		}
	})
}
