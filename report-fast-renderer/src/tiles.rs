//! Map tiles for physical reports.
//!
//! The map used to download every report point in one JSON array, so its
//! memory grew with the total report count. A tile holds only the reports
//! inside one square of the map at one zoom level, grouped into a fixed grid,
//! so its size is capped no matter how many reports exist. The map requests
//! just the tiles in view and drops the ones it leaves.
//!
//! Tiles are Mapbox Vector Tiles (MVT 2.1) with one point layer, `reports`.
//! Each feature is one grid cell that has reports in it:
//! - `count`: reports in the cell
//! - `severity`: highest severity in the cell, rounded to 2 decimals
//! - `lat`, `lon`: exact position, only when `count` is 1 (tile coordinates
//!   are too coarse at low zoom to look a report up by position)
//!
//! The encoder is written by hand: a point-only tile needs four protobuf
//! messages, which is less code than wiring in a protobuf dependency.

use std::collections::HashMap;
use std::f64::consts::PI;

use serde::Serialize;

use crate::model::ReportPoint;

pub const LAYER_NAME: &str = "reports";
pub const MAX_ZOOM: u8 = 22;
/// Tile coordinate range, the MVT default.
const EXTENT: u32 = 4096;
/// Grid cells per tile side. 64 keeps a tile to at most 4,096 features and
/// makes a cell 8 px wide on a 512 px tile, about the size of one dot.
const GRID: usize = 64;
/// Web Mercator cannot show the poles; this is its standard latitude limit.
const MAX_LATITUDE: f64 = 85.051_128_779_806_59;
/// About a tenth of a millimeter, in degrees.
const EDGE: f64 = 1e-9;

/// Position on the Web Mercator world square, both axes in [0, 1).
fn world_position(lon: f64, lat: f64) -> (f64, f64) {
    let x = (lon + 180.0) / 360.0;
    let lat_rad = lat.clamp(-MAX_LATITUDE, MAX_LATITUDE).to_radians();
    let y = (1.0 - (lat_rad.tan() + 1.0 / lat_rad.cos()).ln() / PI) / 2.0;
    (x, y)
}

/// Latitude of a horizontal tile edge; `y` may be one past the last row.
fn tile_edge_latitude(y: u32, tiles_per_side: f64) -> f64 {
    (PI * (1.0 - 2.0 * f64::from(y) / tiles_per_side))
        .sinh()
        .atan()
        .to_degrees()
}

#[derive(Clone, Copy, Default)]
struct Cell {
    count: u64,
    sum_x: f64,
    sum_y: f64,
    max_severity: f64,
    // Position of the first report; exact when it is the only one.
    lat: f64,
    lon: f64,
}

/// Encode tile `z/x/y`. Returns `None` when no report falls inside it.
/// The caller must have checked `z <= MAX_ZOOM` and `x, y < 2^z`.
pub fn build_tile<'a>(
    points: impl Iterator<Item = &'a ReportPoint>,
    z: u8,
    x: u32,
    y: u32,
) -> Option<Vec<u8>> {
    let tiles_per_side = f64::from(1u32 << z);
    let west = f64::from(x) / tiles_per_side * 360.0 - 180.0;
    let east = f64::from(x + 1) / tiles_per_side * 360.0 - 180.0;
    let north = tile_edge_latitude(y, tiles_per_side);
    let south = tile_edge_latitude(y + 1, tiles_per_side);

    let extent = f64::from(EXTENT);
    let cell_size = extent / GRID as f64;
    let mut cells = vec![Cell::default(); GRID * GRID];
    let mut any = false;

    for point in points {
        // Pull the map's outer edges just inside, so a report at longitude 180
        // or beyond the latitude limit still belongs to exactly one tile.
        let longitude = point.longitude.clamp(-180.0, 180.0 - EDGE);
        let latitude = point.latitude.clamp(-MAX_LATITUDE + EDGE, MAX_LATITUDE);
        // Plain comparisons reject most points before the costlier projection.
        if longitude < west || longitude >= east || latitude > north || latitude <= south {
            continue;
        }
        let (world_x, world_y) = world_position(longitude, latitude);
        let tile_x = (world_x * tiles_per_side - f64::from(x)) * extent;
        let tile_y = (world_y * tiles_per_side - f64::from(y)) * extent;
        // Rounding at a tile edge can land one unit outside; keep it inside.
        let column = ((tile_x / cell_size) as usize).min(GRID - 1);
        let row = ((tile_y / cell_size) as usize).min(GRID - 1);
        let cell = &mut cells[row * GRID + column];
        if cell.count == 0 {
            cell.lat = point.latitude;
            cell.lon = point.longitude;
            cell.max_severity = point.severity_level;
        } else if point.severity_level > cell.max_severity {
            cell.max_severity = point.severity_level;
        }
        cell.count += 1;
        cell.sum_x += tile_x;
        cell.sum_y += tile_y;
        any = true;
    }
    if !any {
        return None;
    }

    let mut values = Values::default();
    let mut features = Vec::new();
    for cell in cells.iter().filter(|cell| cell.count > 0) {
        // The dot sits at the average position of the cell's reports.
        let count = cell.count as f64;
        let px = ((cell.sum_x / count).round() as u32).min(EXTENT - 1);
        let py = ((cell.sum_y / count).round() as u32).min(EXTENT - 1);
        let severity = (cell.max_severity.clamp(0.0, 1.0) * 100.0).round() / 100.0;

        let mut tags = vec![KEY_COUNT, values.uint(cell.count)];
        tags.extend([KEY_SEVERITY, values.double(severity)]);
        if cell.count == 1 {
            tags.extend([KEY_LAT, values.double(cell.lat)]);
            tags.extend([KEY_LON, values.double(cell.lon)]);
        }

        let mut feature = Vec::new();
        write_packed(&mut feature, 2, &tags);
        // Geometry type 1 is POINT.
        write_varint_field(&mut feature, 3, 1);
        // 9 is the MoveTo command for one point: (count 1 << 3) | command id 1.
        write_packed(&mut feature, 4, &[9, zigzag(px), zigzag(py)]);
        write_bytes_field(&mut features, 2, &feature);
    }

    let mut layer = Vec::new();
    write_varint_field(&mut layer, 15, 2); // MVT spec version
    write_bytes_field(&mut layer, 1, LAYER_NAME.as_bytes());
    layer.extend_from_slice(&features);
    for key in KEYS {
        write_bytes_field(&mut layer, 3, key.as_bytes());
    }
    layer.extend_from_slice(&values.encoded);
    write_varint_field(&mut layer, 5, u64::from(EXTENT));

    let mut tile = Vec::with_capacity(layer.len() + 8);
    write_bytes_field(&mut tile, 3, &layer);
    Some(tile)
}

/// Property names; a feature refers to one by its position in this list.
const KEYS: [&str; 4] = ["count", "severity", "lat", "lon"];
const KEY_COUNT: u32 = 0;
const KEY_SEVERITY: u32 = 1;
const KEY_LAT: u32 = 2;
const KEY_LON: u32 = 3;

/// The layer's value table. Equal values are stored once and shared.
#[derive(Default)]
struct Values {
    encoded: Vec<u8>,
    // Key: (is a double, raw bits).
    index: HashMap<(bool, u64), u32>,
}

impl Values {
    fn uint(&mut self, value: u64) -> u32 {
        self.intern((false, value), |out| write_varint_field(out, 5, value))
    }

    fn double(&mut self, value: f64) -> u32 {
        self.intern((true, value.to_bits()), |out| {
            write_varint(out, 3 << 3 | 1); // field 3, 64-bit
            out.extend_from_slice(&value.to_le_bytes());
        })
    }

    fn intern(&mut self, key: (bool, u64), encode: impl FnOnce(&mut Vec<u8>)) -> u32 {
        if let Some(&position) = self.index.get(&key) {
            return position;
        }
        let mut value = Vec::new();
        encode(&mut value);
        write_bytes_field(&mut self.encoded, 4, &value);
        let position = self.index.len() as u32;
        self.index.insert(key, position);
        position
    }
}

fn zigzag(value: u32) -> u32 {
    value << 1 // tile coordinates are never negative
}

fn write_varint(out: &mut Vec<u8>, mut value: u64) {
    while value >= 0x80 {
        out.push((value as u8 & 0x7f) | 0x80);
        value >>= 7;
    }
    out.push(value as u8);
}

fn write_varint_field(out: &mut Vec<u8>, field: u32, value: u64) {
    write_varint(out, u64::from(field) << 3);
    write_varint(out, value);
}

fn write_bytes_field(out: &mut Vec<u8>, field: u32, bytes: &[u8]) {
    write_varint(out, u64::from(field) << 3 | 2);
    write_varint(out, bytes.len() as u64);
    out.extend_from_slice(bytes);
}

fn write_packed(out: &mut Vec<u8>, field: u32, values: &[u32]) {
    let mut packed = Vec::with_capacity(values.len() * 2);
    for &value in values {
        write_varint(&mut packed, u64::from(value));
    }
    write_bytes_field(out, field, &packed);
}

/// A place with many reports, for the map's far-view markers.
#[derive(Debug, Clone, PartialEq, Serialize, utoipa::ToSchema)]
pub struct Hotspot {
    pub lon: f64,
    pub lat: f64,
    pub count: u64,
}

/// The `limit` busiest one-degree cells, busiest first, each at the average
/// position of its reports. Ties are ordered west to east, then south to north.
pub fn hotspots<'a>(points: impl Iterator<Item = &'a ReportPoint>, limit: usize) -> Vec<Hotspot> {
    let mut cells: HashMap<(i32, i32), Hotspot> = HashMap::new();
    for point in points {
        if point.latitude.abs() > 85.0 || point.longitude.abs() > 180.0 {
            continue;
        }
        let key = (
            point.longitude.floor() as i32,
            point.latitude.floor() as i32,
        );
        let cell = cells.entry(key).or_insert(Hotspot {
            lon: 0.0,
            lat: 0.0,
            count: 0,
        });
        cell.lon += point.longitude;
        cell.lat += point.latitude;
        cell.count += 1;
    }
    let mut ranked: Vec<((i32, i32), Hotspot)> = cells.into_iter().collect();
    ranked.sort_by(|a, b| b.1.count.cmp(&a.1.count).then(a.0.cmp(&b.0)));
    ranked.truncate(limit);
    ranked
        .into_iter()
        .map(|(_, cell)| Hotspot {
            lon: cell.lon / cell.count as f64,
            lat: cell.lat / cell.count as f64,
            count: cell.count,
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn point(seq: i64, latitude: f64, longitude: f64, severity_level: f64) -> ReportPoint {
        ReportPoint {
            seq,
            severity_level,
            latitude,
            longitude,
        }
    }

    // A minimal MVT reader, so the tests check real decoded content.
    struct Reader<'a>(&'a [u8]);

    impl<'a> Reader<'a> {
        fn varint(&mut self) -> u64 {
            let (mut value, mut shift) = (0u64, 0);
            loop {
                let byte = self.0[0];
                self.0 = &self.0[1..];
                value |= u64::from(byte & 0x7f) << shift;
                if byte < 0x80 {
                    return value;
                }
                shift += 7;
            }
        }
        fn take(&mut self, length: usize) -> &'a [u8] {
            let (head, tail) = self.0.split_at(length);
            self.0 = tail;
            head
        }
        /// Next field as (number, varint value, length-delimited or fixed bytes).
        fn field(&mut self) -> Option<(u64, u64, &'a [u8])> {
            if self.0.is_empty() {
                return None;
            }
            let tag = self.varint();
            Some(match tag & 7 {
                0 => (tag >> 3, self.varint(), &[][..]),
                1 => (tag >> 3, 0, self.take(8)),
                2 => {
                    let length = self.varint() as usize;
                    (tag >> 3, 0, self.take(length))
                }
                other => panic!("unexpected wire type {other}"),
            })
        }
        fn packed(bytes: &'a [u8]) -> Vec<u64> {
            let mut reader = Reader(bytes);
            let mut values = Vec::new();
            while !reader.0.is_empty() {
                values.push(reader.varint());
            }
            values
        }
    }

    #[derive(Debug)]
    struct Decoded {
        x: u64,
        y: u64,
        properties: HashMap<String, f64>,
    }

    fn decode(tile: &[u8]) -> Vec<Decoded> {
        let (number, _, layer) = Reader(tile).field().expect("one layer");
        assert_eq!(number, 3);
        let (mut keys, mut values, mut raw_features) = (Vec::new(), Vec::new(), Vec::new());
        let mut reader = Reader(layer);
        while let Some((number, varint, bytes)) = reader.field() {
            match number {
                15 => assert_eq!(varint, 2),
                1 => assert_eq!(bytes, LAYER_NAME.as_bytes()),
                2 => raw_features.push(bytes),
                3 => keys.push(String::from_utf8(bytes.to_vec()).unwrap()),
                4 => {
                    let (kind, varint, bytes) = Reader(bytes).field().unwrap();
                    values.push(match kind {
                        3 => f64::from_le_bytes(bytes.try_into().unwrap()),
                        5 => varint as f64,
                        other => panic!("unexpected value kind {other}"),
                    });
                }
                5 => assert_eq!(varint, u64::from(EXTENT)),
                other => panic!("unexpected layer field {other}"),
            }
        }
        raw_features
            .into_iter()
            .map(|raw| {
                let mut decoded = Decoded {
                    x: 0,
                    y: 0,
                    properties: HashMap::new(),
                };
                let mut reader = Reader(raw);
                while let Some((number, varint, bytes)) = reader.field() {
                    match number {
                        2 => {
                            for pair in Reader::packed(bytes).chunks(2) {
                                decoded.properties.insert(
                                    keys[pair[0] as usize].clone(),
                                    values[pair[1] as usize],
                                );
                            }
                        }
                        3 => assert_eq!(varint, 1),
                        4 => {
                            let geometry = Reader::packed(bytes);
                            assert_eq!(geometry[0], 9);
                            decoded.x = geometry[1] >> 1;
                            decoded.y = geometry[2] >> 1;
                        }
                        other => panic!("unexpected feature field {other}"),
                    }
                }
                decoded
            })
            .collect()
    }

    #[test]
    fn empty_tile_is_none() {
        let points = [point(1, 48.85, 2.35, 0.5)];
        // Tile 1/0/1 is the south-west quarter of the world; Paris is not in it.
        assert!(build_tile(points.iter(), 1, 0, 1).is_none());
        assert!(build_tile([].iter(), 0, 0, 0).is_none());
    }

    #[test]
    fn single_report_keeps_exact_position() {
        let points = [point(7, 48.8566, 2.3522, 0.437)];
        let features = decode(&build_tile(points.iter(), 0, 0, 0).unwrap());
        assert_eq!(features.len(), 1);
        let feature = &features[0];
        assert_eq!(feature.properties["count"], 1.0);
        assert_eq!(feature.properties["severity"], 0.44);
        assert_eq!(feature.properties["lat"], 48.8566);
        assert_eq!(feature.properties["lon"], 2.3522);
        // Paris is just east of the prime meridian and north of the equator.
        assert_eq!(feature.x, 2075);
        assert_eq!(feature.y, 1409);
    }

    #[test]
    fn nearby_reports_merge_until_zoomed_in() {
        // Two reports about 150 m apart in Zurich, and one in New York.
        let points = [
            point(1, 47.3769, 8.5417, 0.2),
            point(2, 47.3780, 8.5430, 0.9),
            point(3, 40.7128, -74.0060, 0.6),
        ];
        let mut world = decode(&build_tile(points.iter(), 0, 0, 0).unwrap());
        world.sort_by(|a, b| a.x.cmp(&b.x));
        assert_eq!(world.len(), 2);
        assert_eq!(world[0].properties["count"], 1.0); // New York
        assert_eq!(world[1].properties["count"], 2.0); // Zurich
        assert_eq!(world[1].properties["severity"], 0.9);
        assert!(!world[1].properties.contains_key("lat"));

        // At zoom 16 the Zurich reports fall into different tiles or cells.
        let (x, y) = world_position(8.5417, 47.3769);
        let (tile_x, tile_y) = ((x * 65536.0) as u32, (y * 65536.0) as u32);
        let close = decode(&build_tile(points.iter(), 16, tile_x, tile_y).unwrap());
        assert!(close
            .iter()
            .all(|feature| feature.properties["count"] == 1.0));
        assert!(close
            .iter()
            .any(|feature| feature.properties["lat"] == 47.3769));
    }

    #[test]
    fn every_report_lands_in_exactly_one_tile_per_zoom() {
        let points: Vec<ReportPoint> = (0..2000)
            .map(|i| {
                let i = f64::from(i);
                point(
                    i as i64,
                    (i * 0.083).sin() * 84.0,
                    (i * 0.37).cos() * 179.9,
                    0.5,
                )
            })
            .chain([
                point(9001, 0.0001, -180.0, 0.1),
                point(9002, 89.9, 179.999, 0.1),
            ])
            .collect();
        for z in 0..=4u8 {
            let side = 1u32 << z;
            let mut total = 0.0;
            for x in 0..side {
                for y in 0..side {
                    if let Some(tile) = build_tile(points.iter(), z, x, y) {
                        let features = decode(&tile);
                        assert!(features.len() <= GRID * GRID);
                        for feature in &features {
                            assert!(feature.x < 4096 && feature.y < 4096);
                            total += feature.properties["count"];
                        }
                    }
                }
            }
            assert_eq!(total, points.len() as f64, "zoom {z}");
        }
    }

    #[test]
    fn tile_size_is_capped_however_many_reports_exist() {
        let points: Vec<ReportPoint> = (0..200_000)
            .map(|i| {
                let i = f64::from(i);
                point(
                    i as i64,
                    (i * 0.61).sin() * 80.0,
                    (i * 0.17).cos() * 179.0,
                    0.5,
                )
            })
            .collect();
        let tile = build_tile(points.iter(), 0, 0, 0).unwrap();
        assert!(decode(&tile).len() <= GRID * GRID);
        assert!(tile.len() < 100_000, "tile is {} bytes", tile.len());
    }

    #[test]
    fn hotspots_rank_busiest_cells_at_their_average_position() {
        let points = [
            point(1, 47.1, 8.1, 0.1),
            point(2, 47.3, 8.5, 0.1),
            point(3, 47.5, 8.9, 0.1),
            point(4, 40.7, -74.1, 0.1),
            point(5, 40.9, -74.3, 0.1),
            point(6, -33.9, 151.2, 0.1),
            point(7, 89.0, 10.0, 0.1), // too close to the pole to draw
        ];
        let top = hotspots(points.iter(), 2);
        assert_eq!(top.len(), 2);
        assert_eq!(top[0].count, 3);
        assert!((top[0].lat - 47.3).abs() < 1e-9 && (top[0].lon - 8.5).abs() < 1e-9);
        assert_eq!(top[1].count, 2);
        assert!((top[1].lon + 74.2).abs() < 1e-9);
        assert_eq!(hotspots(points.iter(), 10).len(), 3);
        assert!(hotspots(points.iter(), 0).is_empty());
    }
}
