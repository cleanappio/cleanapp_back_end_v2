# Several photos per report

A web report can carry up to **four photos**. Added 2026-11-13 at Boris's
request. Video was requested at the same time and deferred (see the end).

## The rule that keeps everything else working

The **first photo is still `reports.image`**. Every existing reader (image
endpoints, thumbnails, notice emails, the mobile app, exports) sees a report
with one photo exactly as before. Photos 2 to 4 are additional and live in
their own table.

## Path of a photo

1. **Browser** (`cleanapp-frontend`, `src/lib/web-reporting.ts`). Each photo
   is re-encoded as JPEG. All photos of one report share a byte budget so the
   request fits the 2 MiB limit that nginx and the listener enforce: one photo
   may use 1 MiB as before; with four, each gets about 350 KB.
2. **Submit** (`POST /api/v1/human-reports/submit`). `image` is the first
   photo, `extra_images` an array of up to three more (base64, like `image`).
   `validateHumanWebSubmission` checks each one the same way (complete JPEG or
   PNG, at most 1 MiB, 4096 px, 16 MP) and the set together (at most 1.5 MiB).
   Extra photos without a first photo are rejected. The photo hashes are part
   of the report's material identity, so adding a photo makes a new report
   rather than a duplicate.
3. **Wire** (`cleanapp_wire_v1.go`). Extras travel in the
   `extra_images_base64` extension and are inserted into `report_extra_images`
   in the same transaction as the `reports` row. Any Wire source may use the
   extension; it is capped at three images of 1 MiB each.
4. **Analysis** (`report-analyze-pipeline`). Every model call takes one image,
   so the photos are laid out as a contact sheet (`photosheet.Compose`: a
   grid in reading order, each photo scaled to fit a 1024 px cell) and the
   prompt is told so (`photosheet.Note`). The sheet is used for the main
   analysis and the evidence classifier. It is never stored or shown. If the
   extra photos cannot be read or laid out, analysis proceeds on the first
   photo and logs why. The location pass still looks at the first photo only.
5. **Public report**. `GET /api/v3/reports/by-public-id` gains
   `report.extra_image_count` (0 to 3). The photos themselves come from the
   existing raw image endpoint with an index:
   `GET /api/v3/reports/rawimage/by-public-id?public_id=...&index=1` (to 3).
   Without `index` it returns the first photo as before. An indexed photo is
   served under exactly the first photo's rules: the report must be publicly
   visible and its media not withheld for privacy.

## Table

```sql
report_extra_images (seq, position 1..3, image MEDIUMBLOB, sha256, created_at)
PRIMARY KEY (seq, position)
```

Created by the report-listener migration and present in `db/init.sql`. Rows
are written once and never updated.

## Not changed

- Notice emails attach the first photo only.
- Thumbnails, social previews and list views use the first photo only.
- The mobile app sends one photo.

## Video (deferred)

Decisions already taken with Boris for when it is built: analyze still frames
extracted in the browser, and store in the database at first. Open: whether
video plays publicly. It needs a larger request limit than 2 MiB at both nginx
(`client_max_body_size`) and the listener (`REQUEST_BODY_LIMIT_BYTES`), a
server-side check that the bytes really are a video, and range requests for
playback.
