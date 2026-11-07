package database

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/jpeg"
	"strings"
	"testing"

	"cleanapp-common/evidence"
	"github.com/DATA-DOG/go-sqlmock"
)

// APP metadata can precede JPEG dimensions without changing the original image.
func jpegWithAPPMetadata(t *testing.T, width, height, metadataBytes int) []byte {
	t.Helper()
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, width, height)), nil); err != nil {
		t.Fatal(err)
	}
	photo := append([]byte{}, encoded.Bytes()[:2]...)
	for metadataBytes > 0 {
		n := metadataBytes
		if n > 60000 {
			n = 60000
		}
		length := n + 2
		photo = append(photo, 0xff, 0xe2, byte(length>>8), byte(length))
		photo = append(photo, make([]byte, n)...)
		metadataBytes -= n
	}
	return append(photo, encoded.Bytes()[2:]...)
}

func headerGuardMatcher(expected, actual string) error {
	for _, required := range []string{expected, "MAX_EXECUTION_TIME(3000)", PublicVisibilityWhereSQL, "rs.status<>'active'", "ro.is_public=FALSE", "r.seq IN ("} {
		if !strings.Contains(actual, required) {
			return fmt.Errorf("missing bounded live header guard %q", required)
		}
	}
	return nil
}

func TestEvidenceImageHeaderBudget(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(headerGuardMatcher)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	large := jpegWithAPPMetadata(t, 2, 2, 1773000)
	initial := sqlmock.NewRows([]string{"seq", "header"})
	medium := sqlmock.NewRows([]string{"seq", "header"})
	seqs := make([]int, 100)
	for at := range seqs {
		seqs[at] = at + 1
		initial.AddRow(at+1, large[:64<<10])
		medium.AddRow(at+1, large[:512<<10])
	}
	mock.ExpectQuery("SUBSTRING(r.image,1,65536)").WillReturnRows(initial)
	mock.ExpectQuery("SUBSTRING(r.image,1,524288)").WillReturnRows(medium)
	if _, err := loadEvidenceImageValidity(context.Background(), db, seqs); err == nil || !strings.Contains(err.Error(), "128 MiB budget") {
		t.Fatalf("oversized third pass must fail before query: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	seqs = append(seqs, 101)
	if _, err := loadEvidenceImageValidity(context.Background(), db, seqs); err == nil || !strings.Contains(err.Error(), "100 reports") {
		t.Fatalf("unbounded selected IDs must fail before query: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type headerPrivacyChangeReader struct {
	db    *sql.DB
	calls int
}

func (reader *headerPrivacyChangeReader) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	reader.calls++
	if reader.calls == 2 {
		if _, err := reader.db.ExecContext(ctx, `INSERT INTO reports_owners(seq,owner,is_public) VALUES(100,'new-private-owner',FALSE)`); err != nil {
			return nil, err
		}
	}
	return reader.db.QueryContext(ctx, query, args...)
}

func testEvidenceImageHeadersMySQL(t *testing.T, ctx context.Context, db *sql.DB) {
	photos := map[int][]byte{
		100: jpegWithAPPMetadata(t, 2, 2, 120000),
		101: jpegWithAPPMetadata(t, 2, 2, 1773000),
		102: jpegWithAPPMetadata(t, 1, 1, 120000),
		103: bytes.Repeat([]byte{'x'}, 70000),
		104: jpegWithAPPMetadata(t, 2, 2, (2<<20)+60000),
		105: jpegWithAPPMetadata(t, 2, 2, 120000),
		106: jpegWithAPPMetadata(t, 2, 2, 120000),
		107: jpegWithAPPMetadata(t, 2, 2, 120000),
	}
	if evidence.ValidOriginalImage(photos[100][:64<<10]) || evidence.ValidOriginalImage(photos[101][:512<<10]) || !evidence.ValidOriginalImage(photos[101]) || !evidence.ValidOriginalImage(photos[104]) {
		t.Fatal("synthetic JPEGs do not cross the intended real decoder boundaries")
	}
	for seq, photo := range photos {
		if _, err := db.ExecContext(ctx, `INSERT INTO reports(seq,public_id,ts,id,team,latitude,longitude,x,y,image) VALUES(?,?,'2023-07-04 03:52:48','header-fixture',0,10,20,0,0,?)`, seq, fmt.Sprintf("rpt_header_%d", seq), photo); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO report_raw(report_seq,visibility) VALUES(?,'public')`, seq); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{
		`UPDATE report_raw SET visibility='shadow' WHERE report_seq=105`,
		`INSERT INTO reports_owners(seq,owner,is_public) VALUES(106,'private-owner',FALSE)`,
		`INSERT INTO report_status(seq,status) VALUES(107,'resolved')`,
	} {
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	valid, err := loadEvidenceImageValidity(ctx, db, []int{100, 101, 102, 103, 104, 105, 106, 107})
	if err != nil || !valid[100] || !valid[101] || valid[102] || valid[103] || valid[104] || valid[105] || valid[106] || valid[107] {
		t.Fatalf("header expansion/placeholder/bound/publication guard mismatch: %v error=%v", valid, err)
	}
	reader := &headerPrivacyChangeReader{db: db}
	valid, err = loadEvidenceImageValidity(ctx, reader, []int{100})
	if err != nil || valid[100] || reader.calls != 2 {
		t.Fatalf("fallback must recheck ownership changed after first prefix: %v calls=%d error=%v", valid, reader.calls, err)
	}
	// Header validation never normalizes or rewrites the stored original photo.
	var stored []byte
	if err := db.QueryRowContext(ctx, `SELECT image FROM reports WHERE seq=101`).Scan(&stored); err != nil || !bytes.Equal(stored, photos[101]) {
		t.Fatalf("original bytes changed: %v", err)
	}
}
