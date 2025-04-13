import { point, feature, lineString, Position, Properties, Id, BBox, FeatureCollection, Geometry, Point, Feature } from '@turf/helpers';
import distance from '@turf/distance';
import along from '@turf/along';
export declare const makePoint: typeof point;
export declare const makeLineString: typeof lineString;
export declare function makeLatLngBounds(northEastCoordinates: Position, southWestCoordinates: Position): FeatureCollection<Point>;
export declare const makeFeature: typeof feature;
export declare function makeFeatureCollection<G = Geometry, P = Properties>(features?: Array<Feature<G, P>>, options?: {
    bbox?: BBox;
    id?: Id;
}): FeatureCollection<G, P>;
export declare function addToFeatureCollection(newFeatureCollection: FeatureCollection, newFeature: Feature): FeatureCollection;
export declare const calculateDistance: typeof distance;
export declare const pointAlongLine: typeof along;
//# sourceMappingURL=geoUtils.d.ts.map